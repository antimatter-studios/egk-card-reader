//go:build windows

package usb

import "fmt"

type windowsProbe struct{}

func defaultProbe() Probe { return &windowsProbe{} }

func (p *windowsProbe) FindDevices(vendorID, productID uint16) ([]Device, error) {
	// TODO: implement using either:
	//   - golang.org/x/sys/windows + SetupAPI (SetupDiGetClassDevs +
	//     SetupDiEnumDeviceInterfaces + SetupDiGetDeviceRegistryProperty),
	//     filtering on the SetupGUID for COM ports and parsing the
	//     hardware-ID string "USB\VID_xxxx&PID_yyyy".
	//   - WMI via PowerShell: `Get-PnpDevice -Class Ports` and matching
	//     InstanceId substrings (slower but no cgo / no syscalls).
	return nil, fmt.Errorf("%w: TODO for Windows — see usb/windows.go", ErrUnsupported)
}
