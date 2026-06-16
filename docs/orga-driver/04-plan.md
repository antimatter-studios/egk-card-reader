# Investigation plan

## Sequencing

Tracks 0–1 done. Track 2 next. Tracks 3 and 4 in parallel after Track 2.

### Track 0 — enumerate the device ✅ done

Result in [01-hardware.md](01-hardware.md). VID/PID `0780:1202`, CDC-ACM, `/dev/cu.usbmodem11301`.

### Track 1 — hunt for open-source protocol code ✅ done, negative

Result in [03-existing-implementations.md](03-existing-implementations.md). **No open implementation exists.**

### Track 2 — passive serial observation (next)

Open the serial port read-only at common baudrates, watch for any unsolicited bytes (banner, heartbeat, button presses). Tells us whether the device speaks first, default baudrate, and whether keypad/display events trigger out-of-band frames.

Steps:
1. `stty -f /dev/cu.usbmodem11301` to see current line settings.
2. Try opening at 9600, 38400, 115200 (Apraxos says 9600 over USB) and `cat` for 10s. Press buttons on the keypad during the capture.
3. Cycle through 8N1 / 8E1 / 8O1 — eHealth terminals historically prefer **even parity** (T=0 default).
4. Record every byte seen in [05-probe-log.md](05-probe-log.md) with timestamp and baud/parity.

### Track 3 — active probing for RESET CT response

Send candidate framings of CT-BCS `RESET CT` (CLA=20 INS=11 P1=00 P2=00 Lc=00) wrapped as:
- T=1 block: `NAD PCB LEN INF... LRC` with NAD=`0x12` (host→ICC, but we'll try `0x02` host→terminal too)
- Plain APDU (no framing)
- STX-prefixed framing (`02 LEN INF... 03 LRC`)
- TPDU with length prefix only (`LEN INF...`)

Record everything. The RESET CT response is well-defined (`AT ...` ATR-like terminal info) so we can recognize a hit even through corrupted framing.

### Track 4 — Windows capture (parallel to Track 3)

Boot a Windows VM, pass through ORGA, install ORGA driver, run their test tool while capturing USB with USBPcap → Wireshark. Five minutes of traffic = canonical framing.

VM choice: UTM (Apple Silicon, free) running Windows 10 ARM if available, or x86-64 Windows via emulation (slow but fine for USB byte-level work).

### Track 5 — disassembly fallback (only if 3+4 fail)

Pull `libctorgt1_1.4.7_amd64.deb`, extract `libctorgt1.so`, Ghidra, map `CT_data` → byte layout. Clean-room re-implement in Go.

## Implementation target

Once we know the framing, write a Go package `internal/orga/` exposing:

```go
type Terminal struct{ /* fd, mutex */ }
func Open(devNode string) (*Terminal, error)
func (t *Terminal) CTData(dad, sad byte, cmd []byte) (resp []byte, err error)
func (t *Terminal) Close() error
```

Higher layers (CT-BCS helpers, slot management, eGK APDU dispatch) build on this. Eventually plug into the existing `pkg/egk/` so the same APDU code paths work over either Cherry CCID (via `github.com/ebfe/scard`) or ORGA serial.

## Out of scope for this phase

- C2C / gemSpec_COS implementation (the encrypted-reads crypto) — depends on driver first.
- SMC-B CV-cert validity check — needs working driver to talk to the SMC at all.
- PC/SC bridge — only worth doing if we want third-party PC/SC consumers to see the ORGA; not needed for our own Go binary.

## Safety guardrails (cmd/orga-probe)

Mechanical block in `safety.go` refuses these INS bytes unless `-UNSAFE-allow-pin-write` is passed:

- ISO 7816 with non-CT-BCS CLA: `20` VERIFY, `24` CHANGE REFERENCE DATA, `26` DISABLE VERIF REQ, `28` ENABLE VERIF REQ, `2C` RESET RETRY COUNTER, `D6` UPDATE BINARY, `DC` UPDATE RECORD, `DA` PUT DATA, `E0`/`0E` ERASE BINARY, `EE` ERASE RECORD.
- CT-BCS (CLA=20): `16` INPUT, `18` PERFORM VERIFICATION, `19` MODIFY VERIFICATION DATA.

Both the `-apdu` helper and the raw `-tx` path are checked (raw `-tx` is parsed as a T=1 I-block and the INF field inspected). Tests pass: SELECT/READ stay green, all listed dangerous INS exit with status 2.

Rationale: eGK PIN retry counter does not decay over time. Three failed VERIFY → PIN permanently blocked until PUK. The block forces an explicit `-UNSAFE-allow-pin-write` flag so an accidental hex typo can never start a VERIFY chain.
