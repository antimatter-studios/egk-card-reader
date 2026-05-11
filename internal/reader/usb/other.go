//go:build !darwin && !linux && !windows

package usb

type unsupportedProbe struct{}

func defaultProbe() Probe { return &unsupportedProbe{} }

func (p *unsupportedProbe) FindDevices(vendorID, productID uint16) ([]Device, error) {
	return nil, ErrUnsupported
}
