# Framing hypotheses

Working theories about the bytes between `/dev/cu.usbmodem11301` and the terminal. Updated as evidence accumulates. Each hypothesis carries a confidence (low/medium/high) and what would falsify it.

## H1 — Plain CT-BCS APDU, no framing — ❌ FALSIFIED 2026-05-11

> The CDC-ACM endpoint accepts a raw CT-BCS APDU (`20 11 00 00 00`) and returns the response APDU as bytes.

- **Falsified**. Probes 002–007: raw APDU + any random byte string both produced the same fixed 4-byte response `00 81 00 81` — a framing-error NAK rather than any valid CT-BCS reply. The terminal requires block framing.

## H2 — T=1 block (NAD/PCB/LEN/INF.../EDC) at host↔terminal layer ✅ CONFIRMED 2026-05-11

> Host sends a T=1 I-block: `NAD PCB LEN INF[LEN] EDC`. NAD encodes host=0x2, terminal=0x1 (low nibble = SAD, high nibble = DAD, per ISO 7816-3 §11.3.2.1). PCB is I-block `0x00` with N(S)/M bits. LEN is one byte. EDC is LRC (XOR of all preceding bytes).

- **CONFIRMED**. Evidence: probe 008.
- TX `12 00 05 20 11 00 00 00 26` (well-formed T=1 I-block, NAD=host→CT, LEN=5, INF=CT-BCS RESET CT, EDC=LRC).
- RX `21 C3 01 1E FD`:
  - NAD `0x21` = CT(1)→host(2) — terminal correctly swaps nibbles
  - PCB `0xC3` = `1100 0011` = S-block, code 3 = **S(WTX request)** with bit5 cleared (request, not response)
  - LEN `0x01`, INF `0x1E` = WTX multiplier 30 (terminal needs extended waiting time)
  - EDC `0xFD` = XOR(21,C3,01,1E) = FD ✓
- Probes 010/011 also showed: terminal mirrors NAD nibbles back, confirming standard ISO 7816-3 NAD convention.
- **Conclusion**: USB-CDC-ACM transport carries raw T=1 blocks. No additional STX/ETX wrapper, no length prefix, no CRC — pure ISO 7816-3 T=1 with LRC EDC.

## Final framing summary (post-investigation)

The ORGA 930 M speaks **standard ISO 7816-3 T=1** directly on the USB-CDC-ACM endpoint at 9600 8N1, no additional wrapper. Specifically:

- **Block format**: `NAD PCB LEN INF[LEN] EDC` where EDC is a single-byte XOR LRC.
- **NAD encoding**: low nibble = SAD (source address), high nibble = DAD (destination). Host=2, terminal=1, ICC1=0, ICC2=2. Terminal swaps nibbles on its reply.
- **PCB**: standard ISO 7816-3. I-block PCB=`0x00`/`0x40` (N(S)=0/1, M flag bit 5), R-block PCB=`0x80`+`N(R)<<4`+err, S-block PCB=`0xC0|code` for request / `0xE0|code` for response.
- **Supported S-blocks observed**: RESYNCH (code 0), IFS (code 1, terminal requests IFSC=254), WTX (code 3, terminal requests waiting-time extension with multiplier 30 or higher during card operations).
- **Sequence-number state persists across `/dev/cu.usbmodem*` open/close** — the USB-level session is one logical T=1 link. Send S(RESYNCH request) at the start of every host session to safely reset N(S)/N(R).

Response chaining: when a card response exceeds IFSC=254 bytes, the terminal emits I-block with M=1 ("more"), expects an R-block (NAD swapped, PCB=`0x80|(N(R)<<4)`) ack, then sends the continuation. Implemented in `cmd/orga-probe/t1.go::t1Transact`.

The remaining hypotheses below were considered but never tested in depth once H2 was confirmed; preserved for documentary completeness.

## H3 — STX/ETX block protocol

> `0x02 <hdr> <len> <inf> 0x03 <lrc>` — common in 1990s POS/healthcare terminals.

- Confidence: **low-medium**.
- Why considered: ORGA's POS heritage (Sagem Monetel was payment-terminal-first).
- What falsifies: T=1 attempts work first, or STX prefix yields no response.

## H4 — Length-prefixed only

> `<len> <payload>` where len is 1 or 2 bytes.

- Confidence: **low**.
- Why considered: minimal framing that still gives the application layer message boundaries; some custom protocols do this.
- What falsifies: tests yield no response.

## H5 — Sagem proprietary "MEX" / "FT" framing

> Some Sagem terminals (heritage line) use a Sagem-internal block protocol with a multi-byte sync header.

- Confidence: **low**.
- Why considered: vendor heritage.
- What falsifies: T=1 works; we never need to look here.

## Decision rule

Test H2 first because it's the highest-confidence and the cheapest to test. If H2 yields any meaningful response, refine within H2 (NAD encoding, EDC type, etc.). Otherwise sweep H1, H3, H4 in order.
