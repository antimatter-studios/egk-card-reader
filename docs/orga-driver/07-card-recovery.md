# Card recovery after Fatal Error 3 / SW=64A2

A debugging note from an incident on 2026-05-11.

## What happened

During a routine `go run ./cmd/card-reader` session, the ORGA 930 M
terminal displayed **"Fatal Error 3"** on its LCD and rebooted itself.
After re-enumeration, subsequent runs reported either:

- `card-reader: error: orga: T=1 resync: read /dev/cu.usbmodem11301:
  device not configured` (during the post-reboot enumeration window),
  or — once the terminal was back —
- The terminal worked fine for slot 1 (eGK) APDUs, but **every APDU sent
  to slot 2 (the test SMC-B) returned `SW=64A2`**.

The cardholder's eGK in slot 1 was unaffected. Only the slot-2 TCOS test
SMC-B was stuck.

## Root cause (best guess)

`SW=64A2` is not a standard ISO 7816-4 status word. T-Systems TCOS uses
the 64xx range for vendor-specific "card not in operational state"
errors. Best guess: the slot-2 card latched into a transport / preceding
state after the terminal's mid-session power glitch and refuses
application APDUs until it sees a fresh power-up.

## Recovery procedure

A CT-BCS REQUEST ICC with `P2=01` (return ATR) against the terminal
power-cycles the slot and re-activates the card. This recovers the
card from the 64A2-on-everything state.

```
# Issue CT-BCS REQUEST ICC slot=2 P2=01 (return ATR)
./orga-probe -slot 0 -apdu "20 12 02 01 00"

# Expect:
#   RX data: 3bd097ff81b1fe451fc7eb   (the ATR)
#   RX SW:   6201                    (warning, ATR available)

# Now retry SELECT MF — should return 9000 again
./orga-probe -slot 2 -apdu "00 A4 00 0C 02 3F 00"
```

After this, the card responds normally to APDUs. No physical reseat
needed; no need to unplug the terminal.

## What did NOT work

- **EJECT slot 2** (`./orga-probe -slot 0 -apdu "20 15 02 00 00"`)
  returned `SW=6700` "Wrong length". The ORGA 930 M firmware doesn't
  accept that exact APDU shape. Different `Lc` / `Le` combinations
  weren't explored — REQUEST ICC P2=01 worked, so we stopped.
- Closing and reopening `/dev/cu.usbmodem11301` from the host. The card
  state lives on the card chip, not in the terminal's serial buffer or
  the host's file descriptor.

## ENXIO during the reboot window

While the terminal is rebooting, `/dev/cu.usbmodem11301` exists (the
macOS kernel hasn't cleaned the stale node) but the USB endpoint is
gone. Opening the device returns `ENXIO` ("device not configured").

The driver maps this to a friendly error:

```
internal/reader/orga/errors.go::friendlySerialError(syscall.ENXIO)
→ "...the kernel sees the /dev node but the USB endpoint isn't responding.
   If the terminal just rebooted (e.g. after a Fatal Error), wait ~5s
   for re-enumeration. If the device went into DFU mode (PID 0xDF55),
   power-cycle it. Otherwise unplug and replug the USB cable"
```

Waiting ~5 seconds and re-running is usually enough.

## What triggered the Fatal Error in the first place

Unknown. Hypotheses (none confirmed):

1. A specific APDU sequence the terminal didn't like — possibly the
   newly-added CT-BCS `GET STATUS P2=46` in `Session.Identify()`, which
   hadn't been part of the standard flow before. Defense: every T=1
   block is now traceable via `ORGA_TRACE=1`, so the next occurrence
   captures evidence.
2. USB power glitch — the ORGA draws up to 500 mA and some hubs reset
   under load. Plugging directly into the host port (no hub) avoids
   this.
3. Firmware bug in the V5.03/7.05 release. No published changelog.

The trace facility — see
[../orga-driver/04-plan.md](04-plan.md) §Safety guardrails — was added
specifically so a recurrence would have a captured byte log of the
preceding APDUs.

## Defensive measures added 2026-05-11

After this incident, three changes landed:

1. **`ORGA_TRACE=1`** env var logs every T=1 block sent/received to
   stderr with a millisecond timestamp + PCB classification. Implemented
   in [`internal/reader/orga/trace.go`](../../internal/reader/orga/trace.go).
2. **Friendly ENXIO mapping** in
   [`internal/reader/orga/errors.go`](../../internal/reader/orga/errors.go).
   Also covers ENOENT, EACCES, EBUSY.
3. **Fail-fast for `--input orga`** when the USB probe finds no matching
   device. Previously the driver would glob `/dev/cu.usbmodem*` and pick
   any matching node, which after a terminal reboot could be a stale
   leftover. Fix in [`internal/reader/reader.go`](../../internal/reader/reader.go)
   `openORGA`.
