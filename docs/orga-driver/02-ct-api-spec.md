# CT-API + CT-BCS — standardized layers

These are the **published** layers above the proprietary wire framing.

## CT-API (MKT v1.0 Part 3, TeleTrusT 1999)

Three C-ABI entry points. Host-side, transport-agnostic.

```c
char  CT_init (unsigned short ctn, unsigned short pn);
char  CT_data (unsigned short ctn,
               unsigned char *dad, unsigned char *sad,
               unsigned short  lenc, unsigned char *command,
               unsigned short *lenr, unsigned char *response);
char  CT_close(unsigned short ctn);
```

- `ctn` — logical terminal number assigned by host (0..16383)
- `pn`  — physical/port number (vendor-specific; for USB usually a small enum)
- `dad` — destination address byte (see below)
- `sad` — source address byte (always 0x02 = host on outbound)
- `command` / `lenc` — outbound payload
- `response` / `lenr` — inbound payload buffer + length (in/out)

### Address bytes (dad / sad)

| Value | Meaning             |
|-------|---------------------|
| 0x01  | Card terminal (CT)  |
| 0x02  | Host application    |
| 0x00  | ICC1 (primary slot — eGK on ORGA front) |
| 0x02… | ICC2…ICCn (additional slots — SMC on ORGA back) |

Note `0x02` is overloaded (host on sad, ICC2 on dad). Direction disambiguates.

### Return codes

Single signed byte. `0` = OK; negative values are CT-API error codes (`-1 = invalid CTN`, `-8 = device busy`, `-10 = no card`, `-11 = unresponsive card`, etc., per MKT Part 3 §8).

## CT-BCS (MKT v1.0 Part 4)

When `dad = 0x01` (terminal), the payload is a CT-BCS APDU with `CLA = 0x20`. The commands we'll actually need:

| INS  | Command       | Purpose |
|------|---------------|---------|
| 0x11 | RESET CT      | Reset terminal, return ATR-like terminal info |
| 0x12 | REQUEST ICC   | Power up card in slot, return ATR |
| 0x13 | GET STATUS    | Poll slot status (card present/absent/active) |
| 0x14 | EJECT ICC     | Power down / eject |
| 0x16 | INPUT         | Get PIN-pad input (secure PIN entry) |
| 0x17 | OUTPUT        | Drive display |
| 0x18 | PERFORM VERIFICATION | Secure PIN verification on terminal |

P1 typically selects the slot (`0x00` = current, `0x01` = ICC1, `0x02` = ICC2, …).
P2 / data depend on the command.

When `dad ∈ {0x00, 0x02, …}` (a slot), the payload is a **plain ISO 7816-4 APDU** for the card in that slot.

## What's missing

The wire framing — i.e. how a CT-API `command` buffer becomes bytes on `/dev/cu.usbmodem11301` and how response bytes become a `response` buffer — is **not specified** in any open document. TeleTrusT MKT Part 1 §intro:

> "Zwischen HTSI-Modul und MKT liegt die MKT-Interface-Schnittstelle, deren Ausprägung MKT-abhängig ist."
> *(Between the HTSI module and the MKT lies the MKT-Interface, whose form is MKT-dependent.)*

In other words: TeleTrusT explicitly leaves the wire format to each vendor. ORGA's framing lives only inside `ctorg32.dll` (Windows) and `libctorgt1.so` (Linux).

## Sources

- `MKT1-0_T1.pdf` Basiskonzept — TeleTrusT 1999
- `MKT1-0_T3.pdf` CT-API 1.1 — host C ABI
- `MKT1-0_T4.pdf` CT-BCS — terminal command set
- `gemSpec_KT V3.17.0` — gematik profile requiring CT-API conformance
- `gemSpec_MobKT V2.14.0` §11 — APDU list for mobile terminals (RESET CT, REQUEST ICC, …)
