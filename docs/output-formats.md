# Output formats

The card-reader emits a one-shot dump of the current card state to stdout in
several machine-readable formats. Each format flag (`--hl7-fhir`, `--hl7-adt`,
`--gdt`, `--json`) selects one output and
implies sensible defaults; a future sub-parameter syntax will let callers
override those defaults without breaking existing scripts.

## Flags today

| Flag                | Format        | Default variant                                     | Encoding        |
| ------------------- | ------------- | --------------------------------------------------- | --------------- |
| `--hl7-fhir` | HL7 FHIR      | R4, `Bundle` (Patient + Coverage)                   | JSON, UTF-8     |
| `--hl7-adt`  | HL7 v2.x ADT  | v2.5, `ADT^A04` (Register a patient), incl. `PV1`   | UTF-8 (MSH-18)  |
| `--gdt`      | xDT / GDT     | 2.10, Satzart `6301` (Stammdaten übergeben)         | ISO-8859-15     |
| `--json`            | native JSON   | the form-mapping table as JSON (pre-existing)       | UTF-8           |

All formats default to stdout. Redirect with `>` to capture to a file
(e.g. `card-reader --gdt > stamm.gdt`), or use `--file` to write to
`./output/patient-<KVNR>-<timestamp>.<ext>` automatically (the `output/`
directory is created on demand).

## Sub-parameter plan (not yet implemented)

The intent is to grow each flag into a `flag=variant[,key=val,…]` form so
callers can pin a specific dialect without us breaking the default.

```
--hl7-adt=a28
--hl7-adt=a04,version=2.3
--hl7-fhir=r4,profile=kbv-for
--gdt=2.10,satzart=6301
```

Parsing rule: split on `,`; the first token is the primary variant, remaining
tokens are `key=value` pairs. An absent flag picks the documented default.

Until this is wired up, the flags accept no arguments — the rest of this file
just records what the variants *would* be.

## HL7 FHIR (`--hl7-fhir`)

### Default

- Version: R4 (4.0.1)
- Resource shape: a `Bundle` of `type: collection`, holding one `Patient` and
  one `Coverage`.
- `Patient.identifier` system: `http://fhir.de/sid/gkv/kvid-10` (KVNR per
  de.basisprofil.r4)
- `Coverage.payor.identifier` system: `http://fhir.de/sid/arge-ik/iknr` (IKNR)
- Extensions used (de.basisprofil.r4-style URLs, NOT strict KBV profiles):
  - `humanname-namenszusatz`, `humanname-own-prefix` on `Patient.name`
  - `iso21090-ADXP-streetName`, `iso21090-ADXP-houseNumber` on `Address.line`
  - `gkv/versichertenart`, `gkv/besondere-personengruppe`, `gkv/dmp-kennzeichen`,
    `gkv/wop` on `Coverage`
- Encoding: pretty-printed JSON, UTF-8.

### Variants we may want later

| Variant             | Effect                                                           |
| ------------------- | ---------------------------------------------------------------- |
| `r4` (default)      | Base FHIR R4 with de.basisprofil-style extensions                |
| `r4b`               | R4B (4.3.0). Coverage / Patient are unchanged in R4B in practice |
| `r5`                | R5. `Coverage.payor` was renamed to `Coverage.insurer`           |
| `profile=kbv-for`   | Asserts `meta.profile = KBV_PR_FOR_Patient` / `…_Coverage`. Adds the slicing/cardinality those profiles require. Needs a validator round-trip. |
| `profile=basis`     | Asserts `meta.profile = de.basisprofil.r4` Patient/Coverage      |
| `format=xml`        | Emit as FHIR XML instead of JSON                                 |
| `bundle=transaction`| Make it a `transaction` Bundle with `request` entries (POST)     |
| `bundle=none`       | Emit just `Patient` (skip Coverage). Useful for some EMPI senders |

### Notes

- Strict KBV profile conformance is materially more work than base R4 — those
  profiles slice identifiers, mandate specific extension URLs and codings, and
  require fields we don't have on the eGK (e.g. `Patient.address` typed as both
  `postal` and `physical` separately, structured `line` with Strasse/Hausnummer
  extensions on every line). Reach for it only when the receiver is rejecting
  the base output.

## HL7 v2.x ADT (`--hl7-adt`)

### Default

- Version: 2.5 (in `MSH-12`)
- Trigger event: `ADT^A04^ADT_A01` (Register a patient)
- Segments emitted: `MSH`, `EVN`, `PID`, `PV1`, `IN1`
- `MSH-18` (Character Set): `UNICODE UTF-8`
- Segment terminator: `\r\n`. Strict HL7 v2 spec is `\r` only, but every
  mainstream parser (HAPI, Mirth, Cerner, EPIC) accepts `\r\n`, and `\r`-only
  renders as one overwritten line in a terminal — which makes piped output
  look empty. Strip the LFs with `tr -d '\n'` if a strict receiver objects.
- Field separator `|`, encoding chars `^~\&`
- Sender / receiver placeholders: `MSH-3 = CARD-READER`,
  `MSH-4 = PRACTICE`, `MSH-5 = PVS`, `MSH-6 = PRACTICE`. These exist to make
  the message structurally valid and **must be replaced** by the deploying
  practice — likely via env vars or a config file in a follow-up change.

### A04 vs A28 — what actually differs

Same PID and IN1 content. The differences are:

1. `MSH-9` — `ADT^A04^ADT_A01` vs `ADT^A28^ADT_A05`
2. `EVN-1` — `A04` vs `A28`
3. A04 *requires* a `PV1` segment; A28 omits it.

A04 is "patient is being registered for an encounter" (the natural read for a
card swipe). A28 is "add this person to the master person index, no visit
implied" — used by EMPI / DWH systems. Receivers that only care about
demographics treat them identically.

### Variants we may want later

| Variant              | Effect                                                           |
| -------------------- | ---------------------------------------------------------------- |
| `a04` (default)      | Register a patient — includes `PV1`                              |
| `a28`                | Add person information — drops `PV1`                             |
| `a31`                | Update person information — same shape as `A28`, different code  |
| `version=2.3`        | Older v2.3 in MSH-12. Some German labs are still on it           |
| `version=2.5` default| Most widely deployed                                             |
| `version=2.7`        | Adds optional fields; rare in DE                                 |
| `charset=8859/15`    | Sets `MSH-18 = 8859/15` and emits ISO-8859-15 bytes              |
| `charset=ascii`      | 7-bit ASCII; escape umlauts to `\X00C4\` etc.                    |
| `mllp=true`          | Wrap output in MLLP framing (`\x0B…\x1C\x0D`) for socket transport|
| `sender=APP^FAC`     | Override `MSH-3/MSH-4`                                           |
| `receiver=APP^FAC`   | Override `MSH-5/MSH-6`                                           |

### Notes

- KVNR currently goes into `PID-3` (Patient Identifier List) with assigning
  authority `GKV&{IKNR}&IKNR` and code `MR` (Medical Record Number). Some
  receivers prefer `PID-19` (SSN-Number — Patient) for KVNR; that's a variant
  worth considering.
- IKNR is in `IN1-3`. `IN1-2` (Plan ID) is set to `GKV` as a coarse plan label.

## xDT GDT (`--gdt`)

### Default

- Version: 2.10 (`9218 = 02.10`)
- Satzart: `6301` — Stammdaten übergeben (transmit master data)
- Encoding: ISO-8859-15
- Line terminator: `\r\n`
- Line format: `LLL FFFF value \r\n` where `LLL` is the *byte* length of that
  line (including itself and the CR LF), `FFFF` is the 4-digit field code.
- `8100` (Satzlänge) holds the total byte length of the whole record.

### Field codes emitted

| Code | Field                                | Source             |
| ---- | ------------------------------------ | ------------------ |
| 8000 | Satzidentifikation = `6301`          | constant           |
| 8100 | Satzlänge                            | computed           |
| 9218 | GDT-Version = `02.10`                | constant           |
| 0201 | Empfänger-ID                         | placeholder `EMPF` |
| 0203 | Sender-ID                            | placeholder `CRDR` |
| 0205 | Software-Bezeichnung                 | `card-reader`      |
| 3000 | Patientennummer (= KVNR)             | EF.PD              |
| 3101 | Nachname                             | EF.PD              |
| 3102 | Vorname                              | EF.PD              |
| 3103 | Geburtsdatum (TTMMJJJJ)              | EF.PD              |
| 3104 | Titel                                | EF.PD              |
| 3105 | Versicherten-Nr. (KVNR)              | EF.PD              |
| 3106 | Versichertenstatus (1/3/5)           | EF.AVD             |
| 3110 | Geschlecht (1/2/3/4)                 | EF.PD              |
| 3112 | PLZ                                  | EF.PD              |
| 3113 | Wohnort                              | EF.PD              |
| 3114 | Straße + Hausnummer                  | EF.PD              |
| 3116 | Wohnsitzland                         | EF.PD              |
| 4101 | Krankenkasse Name                    | EF.AVD             |
| 4104 | IK der Krankenkasse                  | EF.AVD             |
| 4108 | VKNR                                 | KTDA               |
| 4131 | WOP                                  | EF.AVD             |
| 4133 | Versicherungsschutz Beginn (TTMMJJJJ)| EF.AVD             |
| 4202 | Versicherungsschutz Ende  (TTMMJJJJ) | EF.AVD             |
| 4239 | Karte gelesen am          (TTMMJJJJ) | today              |
| 4242 | Zuzahlung gültig bis      (TTMMJJJJ) | EF.GVD (only if status=1) |

Codes 0201/0203/0205 are placeholders — practice config, must be overridden in
deployment.

### Variants we may want later

| Variant              | Effect                                                           |
| -------------------- | ---------------------------------------------------------------- |
| `2.10` (default)     | GDT 2.10, ISO-8859-15. Universally accepted.                     |
| `3.0`                | GDT 3.0; can opt into UTF-8 (field `9206 = 7`). Adds/changes some field codes; *not* a strict superset of 2.10 — some 2.10-only fields were dropped or renumbered. |
| `satzart=6301`       | Stammdaten übergeben (default)                                   |
| `satzart=6300`       | Stammdaten anfordern — only header + minimal patient ID, used when *requesting* master data from another system. Rarely useful from a card reader. |
| `encoding=utf8`      | (3.0 only) UTF-8 with field `9206 = 7`                           |
| `sender=…`           | Override `0203` (sender GDT-ID) and `0205` (software name)       |
| `receiver=…`         | Override `0201` (receiver GDT-ID)                                |

### 2.10 vs 3.0 — what actually differs

- Same line format (`LLLFFFFvalue\r\n`).
- 3.0 introduced `9206` (charset code) — when set to `7`, the file is UTF-8;
  2.10 receivers don't understand this and assume ISO-8859-15.
- Some field codes were renumbered or dropped between 2.10 and 3.0 — a 3.0
  file can crash a 2.10-only parser if it contains 3.0-only fields.
- A pure-2.10 file (no `9206`, no 3.0-only fields) is *usually* readable by
  3.0-aware systems via backwards compat. Default to 2.10 unless the receiver
  is known to be 3.0.

### Notes

- The 6301 Satzart is what every German PVS that supports GDT will accept for a
  card swipe. The other patient-related Satzarten (`6300` = request master
  data, `6310` = transmit examination results, `6311` = request results) don't
  fit a card-reader's role.
- `LLL` is **byte** length, not character length. With ISO-8859-15, German
  umlauts are 1 byte each, but the encoder must compute the length on the
  encoded bytes — never on the UTF-8 source string. The implementation in
  `pkg/egk/gdt.go` does the encode-then-measure dance for this reason.

## Cross-cutting questions

These apply to all formats and aren't yet decided:

1. **Output destination.** Today: stdout (default) or `--file` (writes
   `./patient-<KVNR>-<ts>.<ext>` in the cwd). Future: optional
   `--file=PATH` for an explicit destination, and an `output.HTTP` /
   `output.MLLP` writer for systems that consume over a socket.
2. **Sender/receiver/practice config.** Today: hardcoded placeholders. Future:
   read from `card-reader.toml` next to the binary, or from environment
   variables (`CARD_READER_HL7_SENDER` etc.).
3. **One-call-emits-multiple.** Today: one flag per invocation. A future
   `--output=hl7-fhir,hl7-adt,gdt` could emit all three sequentially with a
   record separator, useful for testing.
4. **Validation hook.** None today. Optionally, on `--validate`, run the FHIR
   output through a profile validator and the HL7 v2 message through a v2
   validator before printing — purely a developer aid.
