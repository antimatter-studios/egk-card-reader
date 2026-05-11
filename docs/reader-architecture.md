# Reader architecture

How `card-reader` and `orga-probe` talk to physical card hardware.
Updated to reflect the USB-enumeration refactor and the Identify
interface added 2026-05-11.

## Layering

```
cmd/card-reader, cmd/orga-probe
            ↓
internal/reader        ← Session + Card interfaces, factory, Probe
            ↓
   ┌────────┴────────┐
   ↓                 ↓
internal/reader/    internal/reader/
  generic             orga
   ↓                 ↓
PC/SC (CCID)        T=1 over CDC-ACM
ebfe/scard          (own implementation)
   ↓                 ↓
Cherry, OMNIKEY,    ORGA 930 M
REINER cyberJack,   (potentially other
Identiv uTrust,     ORGA 9xx in the
…                   future)

                 internal/reader/usb
                 ↓ ↓ ↓ ↓
                 darwin.go (ioreg)
                 linux.go  (sysfs)
                 windows.go (TODO)
                 other.go   (stub)
```

## Why the split

The original code talked to PC/SC directly via `github.com/ebfe/scard`.
The ORGA terminal is **not** PC/SC — it enumerates as USB-CDC-ACM (a
virtual serial port) rather than USB-CCID (class 11). Wrapping it in a
PC/SC compatibility shim would have been more code than implementing
T=1 directly, and the T=1 framing the ORGA uses is plain ISO 7816-3
(see [orga-driver/](orga-driver/) for the investigation).

After the refactor:

- **`internal/reader.Card`** is the minimal contract every driver
  implements — one method, `Transmit(apdu []byte) ([]byte, error)`.
- **`internal/reader.Session`** is the multi-slot handle returned by
  the factory: `Slot(n) → Card`, `Identify() → DeviceInfo`,
  `Kind() → string`, `Close()`.
- Both [`internal/reader/orga.Slot`](../internal/reader/orga/) and
  [`internal/reader/generic.Card`](../internal/reader/generic/) satisfy
  `Card` structurally; the eGK reader code in `internal/egk` doesn't
  know which is underneath.

## Factory and probe

[`internal/reader.Detect()`](../internal/reader/probe.go) returns a
`*Probe` snapshot of available hardware:

```go
type Probe struct {
    ORGADevices []string     // device paths of detected ORGA terminals
    USBDevices  []usb.Device // full USB descriptors parallel to ORGADevices
    PCSCReaders []string     // PC/SC reader names visible to pcscd
}
```

`Probe.Pick()` returns the highest-priority `Driver` (ORGA before
PC/SC). `Probe.Drivers()` returns all detected drivers in priority order.

`reader.Open(Options{})` runs `Detect().Pick()` then opens the chosen
driver. Force a specific driver via `Options{Force: "orga"}` or
`Options{Force: "generic"}`.

## USB enumeration is OS-specific

ORGA hardware is identified by USB VID `0x0780` + PID `0x1202`
(Ingenico Healthcare GmbH — the "ORGA 900 Smart Card Terminal VCP"
family, which covers the 930 M). Reading USB descriptors to confirm a
device is actually an ORGA — rather than e.g. an Arduino or a generic
CDC-ACM modem — requires OS-specific code:

| OS       | Implementation                                  | File |
|----------|-------------------------------------------------|------|
| macOS    | Parse `ioreg -r -c IOUSBHostDevice -l -w0`      | `internal/reader/usb/darwin.go` |
| Linux    | Read `/sys/bus/usb/devices/<bus>-<port>/`       | `internal/reader/usb/linux.go` |
| Windows  | Stub returning `ErrUnsupported` (TODO SetupAPI) | `internal/reader/usb/windows.go` |
| other    | Stub returning `ErrUnsupported`                 | `internal/reader/usb/other.go` |

Build tags select the right implementation at compile time. All four
files share the same `Probe` interface:

```go
type Probe interface {
    FindDevices(vendorID, productID uint16) ([]Device, error)
}
```

The sysfs approach on Linux deliberately avoids shelling out to `lsusb`
or linking `libusb` — sysfs is part of every modern kernel, requires no
extra dependency, and gives the same descriptor data.

## DeviceInfo: every driver self-describes

`Session.Identify() DeviceInfo` returns:

```go
type DeviceInfo struct {
    Driver          string  // "orga" or "pcsc"
    Manufacturer    string  // USB iManufacturer / vendor prefix
    Product         string  // USB iProduct / reader name
    SerialNumber    string  // USB iSerial; empty for PC/SC
    Device          string  // /dev/cu.usbmodem* for orga; reader name for pcsc
    VendorID        uint16
    ProductID       uint16
    FirmwareInfo    string  // ORGA: CT-BCS GET STATUS terminal info; PC/SC: ATR hex
    SelectionReason string  // why this driver was chosen
}
```

This is printed to stderr by `cmd/card-reader` on every read so the user
can see which device they're actually talking to:

```
Reader detected:
  Driver:        orga
  Manufacturer:  Ingenico Healthcare GmbH
  Product:       ORGA 900 Smart Card Terminal RTM  (PID_DF55 V.07.05)
  Serial:        021500000000DAAA
  Device:        /dev/cu.usbmodem11301
  USB VID/PID:   0x0780 / 0x1202
  Firmware:      FHDEORGMCT93V5.03 7.05
  Reason:        USB descriptor matched ORGA 9xx family (VID 0x0780 / PID 0x1202)
```

## Safety guard at the driver layer

The ORGA driver refuses dangerous APDUs at `Slot.Transmit` time
unless `Options.AllowPINWrite` is set — see [c2c/pin-workflow.md](c2c/pin-workflow.md).
The block covers VERIFY, CHANGE REFERENCE DATA, RESET RETRY COUNTER,
UPDATE BINARY/RECORD, PUT DATA, ERASE BINARY/RECORD, and the CT-BCS
INPUT / PERFORM VERIFICATION / MODIFY VERIFICATION DATA commands that
would drive the terminal pinpad.

The PC/SC driver doesn't currently mirror this guard. If a future use
case routes VERIFY through PC/SC, the same `dangerousISO` filter from
[`internal/reader/orga/safety.go`](../internal/reader/orga/safety.go)
should be promoted to a shared `internal/reader/safety/` package.

## ORGA-specific debugging

| Concern | Where to look |
|---|---|
| Card "stuck" returning SW=64A2 to every APDU | [orga-driver/07-card-recovery.md](orga-driver/07-card-recovery.md) |
| Terminal not detected at all | `ioreg -r -c IOUSBHostDevice -l -w0` and check VID/PID `0780:1202` is present |
| ENXIO on open | Wait ~5s for re-enumeration or unplug/replug. `friendlySerialError` wraps with guidance |
| T=1 traffic log | `ORGA_TRACE=1 ./card-reader --output json --file` |
| Stale `/dev/cu.usbmodem*` node | The USB probe will refuse to use a node that doesn't correspond to a currently-enumerated ORGA, so this is auto-handled |
