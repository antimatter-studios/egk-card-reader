# ORGA 930 M macOS Driver Investigation

Living wiki — read/update/refactor as facts arrive. Don't lose anything; superseded statements stay with a strikethrough and a date.

Last updated: 2026-05-11

## Status: ✅ end-to-end read pipeline working

- Wire framing identified: **T=1 over USB-CDC-ACM at 9600 8N1**, LRC EDC.
- Working host stack: see `cmd/orga-probe/` — handles RESYNCH, IFS request, WTX request, response chaining, and a mechanical block on PIN-decrement / write APDUs.
- Successfully read **EF.PD** from a real eGK in slot 1: SELECT MF → SELECT DF.HCA → SELECT EF.PD → READ BINARY → gzip-decompress → valid gemSpec_eGK XML (673 bytes uncompressed).
- Slot 2 holds an as-yet-unidentified card with no historical bytes in the ATR.

The vendor's claim that ORGA 9xx needs a proprietary Windows-only driver is **misleading**: on macOS the device enumerates as a CDC-ACM virtual COM port via the in-kernel `AppleUSBCDCACM` driver, and the wire payload is standard ISO 7816-3 T=1. Total Go code to drive it: ~250 LoC for the transport + safety layer.

## Pages

- [01-hardware.md](01-hardware.md) — device, USB descriptors, enumeration on macOS
- [02-ct-api-spec.md](02-ct-api-spec.md) — CT-API + CT-BCS standardized layers (host C ABI, terminal command set)
- [03-existing-implementations.md](03-existing-implementations.md) — what exists open-source vs closed-binary
- [04-plan.md](04-plan.md) — investigation plan (tracks 0–3) and current state
- [05-probe-log.md](05-probe-log.md) — chronological log of every byte sent/received during probing
- [06-framing-hypotheses.md](06-framing-hypotheses.md) — working theories about the wire framing, evidence for/against
- [07-card-recovery.md](07-card-recovery.md) — recovering from Fatal Error 3 / SW=64A2 on slot 2 (CT-BCS REQUEST ICC P2=01 trick) + defensive measures landed afterwards
- [cards/](cards/) — per-card identity dumps (`-identify <slot>` output)

See also (one level up):

- [../c2c/](../c2c/) — the C2C handshake implementation and the four-walls model that drives this whole project's design
- [../reader-architecture.md](../reader-architecture.md) — how this driver fits behind the `internal/reader` factory / Session / DeviceInfo abstractions

## Goals

1. Read PIN-protected eGK segments (NFD/DPE/eMP) on macOS using the ORGA 930 M dual-slot reader the user already owns.
2. No new hardware purchases allowed.
3. Implementation in Go, native macOS, no Linux VM if avoidable.

## Hard prerequisites (see project memory)

Even with the ORGA working, protected reads need: live SMC-B with valid CV-certs, cardholder PIN, and a gemSpec_COS C2C implementation. Driver is necessary but not sufficient.
