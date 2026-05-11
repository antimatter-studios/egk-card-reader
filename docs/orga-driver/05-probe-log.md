# Probe log

Append-only chronological record of every byte sent to / received from `/dev/cu.usbmodem11301`. Newest probe at the bottom. Each entry: timestamp, hypothesis being tested, settings (baud/parity/stop), TX bytes, RX bytes, observation, conclusion.

## Format

```
### YYYY-MM-DD HH:MM probe-NNN — short description
- Hypothesis:
- Port: 9600 8N1 / no flow control
- TX (hex):
- RX (hex), bytes, latency:
- Observation:
- Conclusion / next:
```

## Entries

### 2026-05-11 — Session 1: framing discovery (probes 001–011, 9600 8N1)

#### probe-001 — passive 3s listen, no TX
- Hypothesis: device sends banner on open
- Port: 9600 8N1, no flow, dtr=auto (kernel raises on open)
- TX: (none)
- RX: 0 bytes over 3s
- **Conclusion**: device is silent until prompted

#### probe-002 — plain CT-BCS APDU, no framing
- Hypothesis: H1 — raw APDU works
- TX: `20 11 00 00 00`
- RX: `00 81 00 81` (4 bytes, ~1 ms)
- Observation: looks like an R-block-shaped NAK echoed twice

#### probes 003–007 — vary TX, repeat
- TX `20 11 00 00 00`, `00`, `FF`, `AA` — all produced **identical** `00 81 00 81`
- Pure 2s listen with no TX: still 0 bytes
- **Conclusion**: H1 falsified. `00 81 00 81` is a fixed NAK/framing-error response — the terminal is online but rejects our frames

#### probe-008 — H2 T=1 I-block, NAD=0x12 (host→CT), CT-BCS RESET CT
- TX: `12 00 05 20 11 00 00 00 26`
  - NAD=12, PCB=00 (I-block N(S)=0,M=0), LEN=05, INF=`20 11 00 00 00`, LRC=26
- RX: `21 C3 01 1E FD` ✓
  - NAD=21 (CT→host), PCB=C3 = **S(WTX request)** code 3, LEN=01, INF=1E (multiplier 30), LRC=FD
- **Conclusion**: H2 confirmed. T=1 framing is the wire protocol. Terminal asks for extended waiting time — need to send S(WTX response) `12 E3 01 1E EE` and read again

#### probes 009–011 — NAD variants
- TX with NAD `21`, `00`, `10` — terminal mirrored the NAD nibbles back in each response (`21`, `00`, `01`)
- All responses were still S(WTX request) with same INF byte
- **Conclusion**: terminal follows standard ISO 7816-3 NAD swap on response; doesn't enforce a single allowed NAD value at this stage

#### probe-012 — full T=1 RESET CT with auto-WTX handling
- TX: `12 00 05 20 11 00 00 00 26` then auto-`12 E3 01 1E EE` for S(WTX resp)
- RX final: I-block `21 00 02 90 00 B3` → payload `90 00` (SUCCESS)
- **Conclusion**: T=1 stack working end-to-end. Auto-S-ack logic validates against terminal

#### probes 013–017 — second-transaction R-block error
- All CT-BCS commands sent on subsequent runs returned R-block `21 92 00 B3` = R(N=1, err=2)
- **Conclusion**: T=1 N(S)/N(R) state persists across `/dev/cu.usbmodem11301` open/close — USB session is logically a single T=1 link. Closing the FD does not reset T=1 state. Need explicit S(RESYNCH) or N(S) tracking

#### probe-018 — S(RESYNCH request)
- TX: `12 C0 00 D2`, RX: `21 E0 00 C1` = S(RESYNCH response) ✓
- **Conclusion**: RESYNCH resets sequence numbers on both sides. Probe tool now auto-issues RESYNCH before each `-apdu` (controllable via `-resync=false`)

#### probe-019 — RESET CT after RESYNCH, full negotiation
- Observed: S(IFS request INF=`FE`) before S(WTX request). Terminal negotiates IFSC=254 first
- Auto-S-ack works for both IFS and WTX
- **Conclusion**: complete T=1 negotiation handled. Default IFSC negotiated to 254 bytes

#### probe-020 — GET STATUS P1=00 P2=80 (CT info)
- Payload: `80 02 00 03 90 00` → tag-0x80, len 2, value `00 03`, SW=9000

#### probe-021 — GET STATUS P1=01 P2=80 (slot 1 status)
- Payload: `80 01 00 90 00` → slot status byte = `00` → **no card / inactive**

#### probe-022 — GET STATUS P1=02 P2=80 (slot 2 status)
- Payload: `80 01 03 90 00` → slot status byte = `03` → **card present and active**

#### probe-023 — GET STATUS P1=00 P2=46 (manufacturer info / FH)
- Payload (76 bytes):
  ```
  46 48 44 45 4F 52 47 4D 43 54 39 33 56 35 2E 30 33 20 37 2E 30 35
  0F 00 00 00 00 00
  31 31 30 35 32 30 32 36 31 34 34 38 30 37
  00 00 00 00 10 00
  4F 52 47 41 20 39 33 30 20 63 61 72 65 20 20 20 20
  01 45 47 4B 20 39 78 30
  90 00
  ```
  Decoded ASCII regions:
  - `"FHDEORGMCT93V5.03 7.05"` — function-handle tag, country DE, manufacturer code `ORGMCT93`, hardware `V5.03`, firmware `7.05`
  - `"11052026144807"` — RTC: 11.05.2026 14:48:07 (terminal has correct date/time)
  - `"ORGA 930 care    "` — friendly product name (16 chars padded)
  - `"EGK 9x0"` — eGK card application profile name
- **Conclusion**: this is an **ORGA 930 care** running firmware 7.05 on hardware 5.03. Confirms model identification. Terminal carries a real-time clock

#### probe-024 — REQUEST ICC slot 1 (front/eGK)
- Payload: `62 00` → "no card / warning, no info" → **slot 1 is empty**

#### probe-025 — REQUEST ICC slot 2 (back/SMC)
- Payload: `90 01` → SUCCESS, "card was already activated" → **card present and powered**

#### probe-026 — REQUEST ICC slot 2, P2=01 (return ATR)
- Payload: `3B D0 97 FF 81 B1 FE 45 1F C7 EB 62 01`
  - ATR (11 bytes): `3B D0 97 FF 81 B1 FE 45 1F C7 EB`
    - TS=3B (direct), T0=D0 (TA1+TC1+TD1 present, **0 historical bytes**)
    - TA1=97 → Fi=512, Di=64 → max etu freq
    - TC1=FF → no extra guard time
    - TD1=81 → T=1, TD2 follows
    - TD2=B1 → T=1, TA3+TB3+TD3 follow
    - TA3=FE → IFSC=254
    - TB3=45 → CWI=5, BWI=4
    - TD3=1F → T=15 (global), TA4 follows
    - TA4=C7 → clock-stop both states, class A+B+C (5V/3V/1.8V)
    - TCK=EB ✓ (XOR validates)
  - Trailer SW `62 01` = "no information given (warning) — file selected is invalid"… or in CT-BCS context probably "card already powered up, ATR served from cache"
- **Open question**: ATR has **zero historical bytes** which is unusual for a healthcare card. eGK and SMC-B G2 ATRs normally embed historical bytes with the application profile ID. Possibilities:
  - the card is **not a healthcare card** (test card, JavaCard, dev card)
  - the card pre-dates G2 and uses pre-personalization
  - ORGA strips historical bytes when serving cached ATR (test by SELECT MF / read manufacturer EF)

#### probe-027 — REQUEST ICC slot 2, P2=81 (full ATR)
- Payload: `6A 00` = "Wrong parameter P1/P2"
- **Conclusion**: ORGA 930 care does not implement P2=81 for REQUEST ICC. P2=01 is the canonical "return ATR" form on this terminal

### 2026-05-11 — Session 2: eGK in slot 1

#### probe-028 — REQUEST ICC slot 1 (eGK now inserted), P2=01
- Payload: `3B D0 97 FF 81 B1 FE 45 1F 07 2B 90 01`
  - ATR (11 bytes): `3B D0 97 FF 81 B1 FE 45 1F 07 2B`
  - TS=3B, T0=D0 (TA1+TC1+TD1, **0 historical bytes**), TA1=97, TC1=FF, TD1=81 (T=1), TD2=B1 (T=1), TA3=FE (IFSC=254), TB3=45 (CWI=5 BWI=4), TD3=1F (T=15), **TA4=07** (no clock-stop, class A+B+C), TCK=2B ✓
  - Note: ATR differs from slot-2 card only in TA4 (07 vs C7) and consequently TCK. Same Fi/Di/IFSC/CWI/BWI parameters
  - SW `9001` = "card was already activated"

#### probe-029 — SELECT MF (3F00) over slot 1
- TX APDU: `00 A4 00 0C 02 3F 00` (P1=00 select by FID, P2=0C no response data)
- T=1 outbound NAD=`02` (host→ICC1), response NAD=`20` (ICC1→host) — confirms NAD swap also works for ICC routing
- RX SW: `90 00` ✓

#### probe-030 — SELECT EF.GDO (2F02)
- TX: `00 A4 02 0C 02 2F 02` (P1=02 select EF by FID under current DF)
- RX: `90 00`

#### probe-031 — READ BINARY EF.GDO (10 bytes + SW)
- TX: `00 B0 00 00 00` (offset 0, Le=0 = 256 max)
- RX payload: `5A 0A 80 27 60 00 00 00 00 00 00 00 90 00`
  - BER-TLV tag `5A`, length `0A`, value: 10-byte **ICCSN `80276000000000000000`**
  - SW=9000
- **Conclusion**: first real eGK data successfully read off the card via the ORGA on macOS

#### probe-032 — SELECT DF.HCA by AID
- TX: `00 A4 04 0C 06 D2 76 00 00 01 02` (gematik AID for Healthcare Application)
- RX: `90 00`

#### probe-033 — SELECT EF.PD (D001) under DF.HCA
- TX: `00 A4 02 0C 02 D0 01`
- RX: `90 00`

#### probe-034 — READ BINARY EF.PD first 8 bytes
- TX: `00 B0 00 00 08`
- RX payload: `01 91 1F 8B 08 00 00 00 90 00`
  - 2-byte length prefix `01 91` = 0x0191 = **401 bytes compressed payload follow**
  - `1F 8B 08 00 00 00` = gzip magic + method 8 (deflate) + flags 0 + mtime 0
- **Conclusion**: EF.PD is gzipped XML as expected per gemSpec_eGK

#### probes 035–037 — full EF.PD body via two 250-byte reads
- TX1: `00 B0 00 00 FA` → 250 data + SW 9000
- TX2: `00 B0 00 FA FA` → 153 data + SW 6282 ("end of file before Le bytes")
- Decompressed (gzip, 673 bytes uncompressed): valid XML rooted at `UC_PersoenlicheVersichertendatenXML` per gemSpec_eGK Patient Master Data schema, encoding ISO-8859-15
- Tags present (counts all = 1): `Versicherter`, `Versicherten_ID`, `Person`, `Geburtsdatum`, `Vorname`, `Nachname`, `Geschlecht`, `StrassenAdresse`, `Postleitzahl`, `Ort`, `Land`, `Wohnsitzlaendercode`, `Strasse`, `Hausnummer`, `Anschriftenzusatz`
- **Conclusion**: complete read pipeline functional — ORGA on macOS unlocked for unauthenticated eGK reads. Values not extracted into this log (PII)
