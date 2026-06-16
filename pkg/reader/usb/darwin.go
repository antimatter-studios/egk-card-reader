//go:build darwin

package usb

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type darwinProbe struct {
	// ioregOutput is the function that produces the raw ioreg listing.
	// Overridable for tests so we don't need to shell out.
	ioregOutput func() ([]byte, error)
}

func defaultProbe() Probe {
	return &darwinProbe{
		ioregOutput: func() ([]byte, error) {
			return exec.Command("ioreg", "-r", "-c", "IOUSBHostDevice", "-l", "-w0").Output()
		},
	}
}

func (p *darwinProbe) FindDevices(vendorID, productID uint16) ([]Device, error) {
	out, err := p.ioregOutput()
	if err != nil {
		return nil, fmt.Errorf("usb/darwin: ioreg: %w", err)
	}
	return parseIORegDevices(string(out), vendorID, productID), nil
}

var (
	deviceLineRE = regexp.MustCompile(`^([| ]*)\+-o `)
	propIntRE    = regexp.MustCompile(`"(\w+)"\s*=\s*(-?\d+)`)
	propStringRE = regexp.MustCompile(`"(\w+)"\s*=\s*"([^"]*)"`)
)

// parseIORegDevices walks an ioreg listing and returns every IOUSBHostDevice
// whose idVendor / idProduct match the filter, populated with the strings
// kUSBVendorString / kUSBProductString / kUSBSerialNumberString found in the
// same frame and the IOCalloutDevice path from a descendant IOSerialBSDClient.
//
// The format is an indented tree where lines like
//
//	  | | +-o ORGA 900 Smart Card Terminal RTM ... <class IOUSBHostDevice, ...>
//
// open a new node at depth proportional to the leading "| " / "  " run.
// Properties of a node live in {…} blocks indented further. Children appear
// at deeper indent. A node's scope ends at the next "+-o" with indent ≤ its
// own.
//
// Implementation: a stack of frames keyed by indent. On each new "+-o",
// pop frames whose indent ≥ ours (closing their scope and emitting if
// they matched). Property lines update the top frame; IOCalloutDevice
// propagates up to the nearest IOUSBHostDevice ancestor in the stack.
func parseIORegDevices(out string, vid, pid uint16) []Device {
	type frame struct {
		indent     int
		isUSBDev   bool
		matchedVID bool
		matchedPID bool
		dev        Device
	}
	var stack []*frame
	var results []Device

	emit := func(f *frame) {
		if f.isUSBDev && f.matchedVID && f.matchedPID && f.dev.DevicePath != "" {
			f.dev.VendorID = vid
			f.dev.ProductID = pid
			results = append(results, f.dev)
		}
	}
	closeUpTo := func(target int) {
		for len(stack) > 0 && stack[len(stack)-1].indent >= target {
			emit(stack[len(stack)-1])
			stack = stack[:len(stack)-1]
		}
	}

	wantVID := fmt.Sprintf("%d", vid)
	wantPID := fmt.Sprintf("%d", pid)

	for _, ln := range strings.Split(out, "\n") {
		if m := deviceLineRE.FindStringSubmatch(ln); m != nil {
			closeUpTo(len(m[1]))
			stack = append(stack, &frame{
				indent:   len(m[1]),
				isUSBDev: strings.Contains(ln, "<class IOUSBHostDevice,"),
			})
			continue
		}
		if len(stack) == 0 {
			continue
		}
		if m := propIntRE.FindStringSubmatch(ln); m != nil {
			top := stack[len(stack)-1]
			if !top.isUSBDev {
				continue
			}
			switch m[1] {
			case "idVendor":
				if m[2] == wantVID {
					top.matchedVID = true
				}
			case "idProduct":
				if m[2] == wantPID {
					top.matchedPID = true
				}
			}
		}
		if m := propStringRE.FindStringSubmatch(ln); m != nil {
			// Property values may live on a child frame (e.g. the {…} block
			// of the device itself, or an IOSerialBSDClient descendant for
			// IOCalloutDevice). Always attach to the nearest IOUSBHostDevice.
			for j := len(stack) - 1; j >= 0; j-- {
				if !stack[j].isUSBDev {
					continue
				}
				switch m[1] {
				case "kUSBVendorString":
					stack[j].dev.Manufacturer = m[2]
				case "kUSBProductString":
					stack[j].dev.Product = m[2]
				case "kUSBSerialNumberString":
					stack[j].dev.Serial = m[2]
				case "IOCalloutDevice":
					stack[j].dev.DevicePath = m[2]
				}
				break
			}
		}
	}
	closeUpTo(-1)
	return results
}
