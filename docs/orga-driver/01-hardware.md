# Hardware — ORGA 930 M

## Device

- Model marking: **ORGA 930 M** (Ingenico Healthcare GmbH; brand lineage: Sagem Orga → Orga Kartensysteme → Ingenico → Worldline).
- Dual-slot: front contact slot (eGK), back/SMC slot (SMC-B / HBA).
- Has a PIN-pad keypad and LCD display.

## USB descriptors (observed on macOS, 2026-05-11)

```
USB Vendor Name : Ingenico Healthcare GmbH
idVendor        : 0x0780  (1920 — Sagem Monetel; reassigned to Ingenico)
idProduct       : 0x1202  (4610 — "ORGA 900 Smart Card Terminal Virtual Com Port")
bcdDevice       : 0x0503  (firmware v5.03)
USB Product Name: "ORGA 900 Smart Card Terminal RTM  (PID_DF55 V.07.05)"
USB Serial No.  : 021500000000DAAA
bDeviceClass    : 2  (Communications — CDC-ACM, NOT CCID class 11)
bDeviceSubClass : 0
bDeviceProtocol : 0
bcdUSB          : 0x0200
USBSpeed        : Full Speed (12 Mbit/s)
```

The "PID_DF55" string in the product name is informational — DF55 is the **DFU bootloader** PID; the device is currently in normal runtime (RTM) mode with PID 0x1202. RTM firmware tag is `V.07.05`.

## macOS enumeration

- CDC-ACM is handled by the kernel's built-in `AppleUSBCDCACM` driver.
- Created serial node: **`/dev/cu.usbmodem11301`** (callout) and `/dev/tty.usbmodem11301` (dial-in).
- No vendor driver / kext required for the serial layer — it's a virtual COM port.

## What this means

- We can read/write bytes to `/dev/cu.usbmodem11301` from any process (no special USB permissions needed, no codesigning).
- The proprietary part is **what bytes to send** and **how they're framed**. CDC-ACM is just the transport.
- Linux equivalent would be `/dev/ttyACM0` — Apraxos install docs confirm ORGA 920/930 M uses `/dev/ttyACM0` with the closed-source `libctorgt1` SO at 9600 baud (over USB; serial cable variant is 115200 8N1).

## Family / family compatibility

PID 0x1202 covers the full ORGA 9xx family (900, 920, 930, 930 M, 6000 also uses 0x1302/0x1303). Any open implementation targeting any ORGA 9xx is likely framing-compatible with the 930 M, but no such implementation has been found (see [03-existing-implementations.md](03-existing-implementations.md)).
