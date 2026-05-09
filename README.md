# card-reader

Reads a German eGK (elektronische Gesundheitskarte) over PC/SC, decodes the public insurance data, and re-emits it in any of four clinical-exchange formats (GKV billing form, GDT 2.10, HL7 v2.5 ADT, HL7 FHIR R4) — or parses one of those formats back from disk and shows you what it understood.

Read once, render, exit. No TUI, no input loop.

## Requirements

- **A real CCID smart-card reader** (USB). Generic SD/MMC readers are *not* smart-card readers — even if macOS shows them via PC/SC they cannot power the eGK chip. If your card status reads `PRESENT|MUTE` with an empty ATR, that's the symptom.
- **macOS / Windows**: PC/SC is built into the OS.
- **Linux**: `sudo apt install pcscd pcsc-tools libpcsclite-dev` and `sudo systemctl start pcscd`.
- File-input mode (`--input <path>`) does not need a reader.

## Build

```sh
go build -o card-reader ./cmd/card-reader
```

## Architecture at a glance

```
┌─────────────────────┐       ┌───────────────────────┐       ┌──────────────────────┐
│  Input acquisition  │       │  Enrichment           │       │  Rendering           │
│                     │       │                       │       │                      │
│  ┌───────────────┐  │       │  IKNR ──┐             │       │  ┌────────────────┐  │
│  │ PC/SC reader  │──┼──┐    │         ▼             │       │  │ comprehension  │  │
│  └───────────────┘  │  │    │    ktda.json          │       │  │ table (stdout) │  │
│  ┌───────────────┐  │  │    │  ┌─────────────────┐  │       │  └────────────────┘  │
│  │ .gdt file     │──┼──┤    │  │ IK → Name,      │  │       │  ┌────────────────┐  │
│  └───────────────┘  │  │    │  │      VKNR,      │  │       │  │ raw-bytes file │  │
│  ┌───────────────┐  │  ├──▶ │  │      Kassenart, │ ─┼──┬──▶ │  │ ./output/...   │  │
│  │ .hl7 file     │──┼──┤    │  │      validity,  │  │  │    │  └────────────────┘  │
│  └───────────────┘  │  │    │  │      links      │  │  │    └──────────────────────┘
│  ┌───────────────┐  │  │    │  └─────────────────┘  │  │
│  │ .fhir.json    │──┼──┘    │         │             │  │
│  └───────────────┘  │       │         ▼             │  │
└─────────────────────┘       │   IKInfo {Name,VKNR,  │  │
                              │    Kassenart,KTG}     │  │
                              └───────────┬───────────┘  │
                                          ▼              │
                                   CardData + IKInfo ────┘
                                   (one shape, four renderers)
```

1. **Acquire** — read live over PC/SC, or parse a previously written `.gdt` / `.hl7` / `.fhir.json`. Both paths land in the same internal `CardData` shape (see [internal/egk/egk.go](internal/egk/egk.go)).
2. **Enrich** — the eGK carries the insurer's `IKNR` and display name, but not the `VKNR`, `Kassenart`, or `Kostenträgergruppe` that German practice-management forms demand. Those come from `ktda.json`, a compiled lookup table derived from six quarterly KE0 files (see [KTDA — the insurer lookup table](#ktda--the-insurer-lookup-table)).
3. **Render** — the same `CardData + IKInfo` is fed to one of five encoders (`form`, `gdt`, `hl7-fhir`, `hl7-adt`, `json`). Output goes to a styled lipgloss "comprehension" table on stdout (verify what was parsed), or to `./output/patient-<KVNR>-<timestamp>.<ext>` as raw bytes.

The `Encoder` and `Writer` interfaces ([internal/document/document.go](internal/document/document.go), [internal/output/output.go](internal/output/output.go)) keep *what* the document looks like decoupled from *where* it goes — adding a new format is one Encoder and one registry line.

## How the eGK read pipeline works

The eGK is a Java-Card chip card. The `internal/egk` package talks to it via the OS PC/SC stack with raw ISO 7816-4 APDUs.

```
PC/SC → SELECT MF (3F00, optional)
      → SELECT DF.HCA by AID (D2 76 00 00 01 02)
      → READ BINARY EF.PD (SFI 0x01 / FID 2F01)  ─┐
      → READ BINARY EF.VD (SFI 0x02 / FID 2F02)  ─┤
                                                   ▼
                                       raw bytes (chunked, ≤252/call)
                                                   │
                                          ┌────────┴──────────┐
                                          ▼                   ▼
                                       EF.PD               EF.VD
                                  ┌──────────────┐  ┌────────────────────┐
                                  │ length (2B)  │  │ 4× offset pointers │
                                  │ gzipped XML  │  │ AVD start/end      │
                                  └──────┬───────┘  │ GVD start/end      │
                                         │          │ gzipped XML × 2    │
                                         ▼          └─────────┬──────────┘
                                       gunzip                 ▼
                                         │                  gunzip × 2
                                         ▼                    │
                                    ISO-8859-15 XML           ▼
                                         │             ISO-8859-15 XML
                                         ▼                    │
                              encoding/xml + charmap          ▼
                                         │             encoding/xml + charmap
                                         ▼                    │
                                   PersonalData       InsuranceData (AVD)
                                                      ProtectedData (GVD)
```

What each layer is for:

- **APDUs** ([internal/egk/apdu.go](internal/egk/apdu.go)) — ISO 7816-4 commands. `SELECT AID` switches into the Health Card Application; `READ BINARY` pulls the file body. The reader prefers SFI-based access (one APDU = SELECT + READ in a single shot) and only falls back to FID-based `SELECT EF` when SFI fails. SFI mode caps the file offset at 255, so once the read passes that boundary the implementation drops back to plain READ BINARY against the implicit current EF (see [internal/egk/apdu.go:180-211](internal/egk/apdu.go#L180-L211)).
- **Files** ([internal/egk/egk.go](internal/egk/egk.go)) — only two are read, both unprotected by PIN:
  - `EF.PD` (`2F01`) — *Persönliche Versichertendaten* — name, DOB, gender, address.
  - `EF.VD` (`2F02`) — packs *AVD* (Allgemeine Versicherungsdaten) + *GVD* (Geschützte Versichertendaten). The first 8 bytes are four big-endian pointers (`AVD-start, AVD-end, GVD-start, GVD-end`) into the rest of the file; each section is independently gzipped XML.
- **Decompress + parse** ([internal/egk/parse.go](internal/egk/parse.go)) — gunzip, then `encoding/xml` with a `CharsetReader` that resolves `ISO-8859-15` (the gematik-declared charset) via `golang.org/x/text/encoding/charmap`. The schemas are gematik `gemSpec_eGK_Fach` v5.2 — `UC_PersoenlicheVersichertendatenXML`, `UC_AllgemeineVersicherungsdatenXML`, `UC_GeschuetzteVersichertendatenXML`.

What we get out:

| Source | Field | Used for |
| --- | --- | --- |
| `EF.PD` | Versicherten_ID (KVNR), names, title, gender, DOB, full address | Patient demographics in every output format |
| `EF.AVD` | InsurerID (issuing IK), BillingInsurerID (settling IK), insurer name, cover dates, Versichertenart, WOP, DMP-Kennzeichen, Besondere Personengruppe | Insurance / billing block |
| `EF.GVD` | Zuzahlungsstatus + Gültig_bis, Selektivverträge | Co-pay exemption row |

`EF.AVD` carries two IKs. `Kostentraeger.Kostentraegerkennung` is the *issuing* insurer (e.g. a regional TK office); `AbrechnenderKostentraeger.Kostentraegerkennung` is the IK that actually settles billing. German practice-management forms expect the *billing* IK; the form mapping prefers it and falls back to the issuing IK only if the card omits the `AbrechnenderKostentraeger` block.

## KTDA — the insurer lookup table

The eGK does not carry everything a practice needs to bill. In particular, it does **not** carry:

- **VKNR** (Vertragskassennummer) — the 5-digit code many PVS forms still require alongside the IK.
- **Kassenart** — which family of statutory insurer this is (AOK, BKK, IKK, Knappschaft, SVLFG, Ersatzkassen).
- **Kostenträgergruppe** — the 2-digit KBV billing-form code derived from the Kassenart (Anlage 6 BMV-Ä).
- **Validity, links, abrechnungsstellen** — used for sanity-checking the read.

These come from the *Kostenträgerdatei*, a public dataset published quarterly by `gkv-datenaustausch.de` as six UN/EDIFACT files (one per Kassenart), file extension `.ke0`. The card-reader downloads, parses, and merges them into a single `ktda-files/ktda.json`.

```
┌─────────────────────────────┐
│ gkv-datenaustausch.de       │  the index page lists current quarter's KE0 files;
│   /leistungserbringer/...   │  filenames change every quarter (Q1 → Q4)
└──────────────┬──────────────┘
               │  HTTP GET (HTML scrape)
               ▼
        DiscoverFiles()                  internal/ktda/fetch.go
               │
               │  6 KE0 URLs (AO/EK/BK/IK/BN/LK + verfahren 05 + Qq + yy)
               ▼
        Download(urls, dir)              internal/ktda/fetch.go
               │
               │  6× ISO-8859-1 EDIFACT-style binary segments
               ▼
        ktda-files/raw/
        ├── AO05Q226.ke0        AOK-family insurers
        ├── EK05Q226.ke0        Ersatzkassen (TK, Barmer, DAK, …)
        ├── BK05Q226.ke0        BKKs
        ├── IK05Q226.ke0        IKKs
        ├── BN05Q226.ke0        Knappschaft
        └── LK05Q226.ke0        SVLFG / landwirtschaftliche KK
               │
               │  for each file: open, ISO-8859-1 decode, segment-split
               ▼
        Parse(r, kassenart)              internal/ktda/ke0.go
               │
               │  one Entry per UNH...UNT message, populated from:
               │    IDK  → IK, ShortName, VKNR (if present)
               │    VDT  → ValidFrom, ValidTo
               │    NAM  → Name (joined Name1..Name4)
               │    VKG  → Links (Verknüpfungen to billing/data IKs)
               ▼
        []Entry × 6
               │
               │  merge by IK; on conflict prefer "has VKNR" then later ValidFrom
               ▼
        Compile(allEntries, sources)     internal/ktda/store.go
               │
               ▼
        ktda-files/ktda.json
        {
          "generated_at": "2026-04-29T15:39:00Z",
          "source": "GKV-Datenaustausch …",
          "sources": ["AO05Q226.ke0", …],
          "by_ik": {
            "109519005": { "IK":…, "Name":…, "VKNR":…,
                           "Kassenart":"EK", "ValidFrom":…, "Links":[…] },
            …
          }
        }
```

### Web fetch — what `ktda update` actually does

[internal/ktda/fetch.go](internal/ktda/fetch.go) is intentionally minimal — no HTML parser dependency:

1. **`DiscoverFiles()`** — `GET https://www.gkv-datenaustausch.de/leistungserbringer/sonstige_leistungserbringer/kostentraegerdateien_sle/kostentraegerdateien.jsp`, then a regex pass over the body for `href="…\.ke0"`. Two regexes: one to pull every `.ke0` href out of the HTML, one to filter the basenames against `(AO|EK|BK|IK|BN|LK)\d{2}Q\d\d{2}\.ke0`. Hardcoded filenames would rot every quarter — scraping the index keeps the tool current as long as the page structure holds.
2. **`Download(urls, dir)`** — sequential `GET`s into `ktda-files/raw/`, each via `<dst>.tmp` + atomic rename so a crashed download doesn't leave a half-file in place.
3. **`Parse(r, kassenart)`** — see below.
4. **`Compile(allEntries, sources)`** — merge into a single `Store` keyed by IK, write pretty-printed JSON to `ktda-files/ktda.json`.

### KE0 — what's in the wire format

KE0 is UN/EDIFACT. Charset is ISO-8859-1. Segments end with `'` (apostrophe), fields are separated by `+`, sub-fields by `:`. A `?` is the EDIFACT release character — `?+` is a literal `+`. The parser at [internal/ktda/ke0.go](internal/ktda/ke0.go) handles only the segments we need:

| Segment | Meaning | What the parser keeps |
| --- | --- | --- |
| `UNB` / `UNZ` | Interchange envelope | (ignored) |
| `UNH` / `UNT` | Message header / trailer | start / end of one IK record |
| `IDK` | Identifier — IK + Kurzbezeichnung + (sometimes) VKNR | `IK`, `ShortName`, `VKNR` |
| `VDT` | Validity dates (`YYYYMMDD` / `YYYYMMDD`) | `ValidFrom`, `ValidTo` |
| `NAM` | Long name in up to four 30-char chunks | joined into `Name` |
| `VKG` | *Verknüpfung* — pointers to other IKs that handle billing or data acceptance for this insurer | `Links[]` (Art / partner-IK / Leistungserbringergruppe / Abrechnungsstelle) |

`VKG.Art` is the link type:
- `01` — *Kostenträger* — the actual settling insurer for cards bearing this IK
- `02` / `03` — *Datenannahme* (paper / electronic)
- `09` — *Papier-Annahmestelle*

Most card-bearing IKs map straight to themselves via `Art=01`; the link records exist for cases where a regional office issues cards but a head office bills.

### Compile step — why it's not just "save the parses"

Six KE0 files yield ~30,000 raw `Entry` records, but most IKs appear in only one file (each Kassenart owns its prefix range). A handful do appear twice — different validity periods, or the same IK published in two adjacent quarters during a transition. `Compile()` keys by `IK` and on conflict picks:

1. The entry with a non-empty `VKNR` (sometimes only one of the duplicates carries it).
2. Otherwise the one with the later `ValidFrom`.

The output is a single `map[IK]Entry` and a small header recording when this was compiled and which files fed it. That's what `ktda.json` is: not a copy of the source, but a flattened, deduplicated lookup table tuned for `O(1)` IK lookup at card-read time.

### Runtime use

At read time, `resolveIK()` ([cmd/card-reader/ktda_cmd.go:164](cmd/card-reader/ktda_cmd.go#L164)) takes the card's `BillingInsurerID` (or `InsurerID` if the card omits the billing block), loads the on-disk `ktda.json` (auto-fetching on first run), looks up the entry, and returns an `egk.IKInfo{Name, VKNR, Kassenart, KostentraegerGruppe}`. That `IKInfo` is then handed to whichever encoder is active — every output format reads from the same struct, so an enriched form table, an enriched GDT 4108 field, and an enriched FHIR `Coverage.payor` all stay in sync without coordination.

If `ktda.json` is missing and the auto-fetch fails (offline), the form still renders; affected fields show empty values with a `run \`card-reader ktda update\`` note.

### Cadence

KE0 files are republished **quarterly** (1.1., 1.4., 1.7., 1.10.) — re-run `ktda update` each quarter to pick up insurer mergers, new IK assignments, and ended insurers. The form-render path warns on stderr when the loaded `ktda.json` was compiled from a quarter older than the current one, but doesn't block — stale data still resolves the vast majority of IKs.

```sh
card-reader ktda update [DIR]   # download + compile, write ktda-files/ktda.json
card-reader ktda lookup IK      # print one IK's full record
card-reader ktda info           # path, file count, generation timestamp
```

## Reference tables

Five static reference tables sit between raw card values and human-readable output. Four are tiny constants baked into the binary; one (KTDA) is the on-disk JSON described above.

### 1. Kassenart → Kostenträgergruppe (2-digit KBV form code)

KBV Anlage 6 BMV-Ä. Hardcoded in [internal/ktda/store.go:108-124](internal/ktda/store.go#L108-L124):

| Kassenart prefix | Family | Kostenträgergruppe |
| --- | --- | --- |
| `AO` | AOK | `01` |
| `BK` | BKK | `02` |
| `IK` | IKK | `03` |
| `BN` | Knappschaft (BAHN-BKK family) | `05` |
| `EK` | Ersatzkassen (vdek — TK, Barmer, DAK, …) | `06` |
| `LK` | SVLFG / landwirtschaftliche KK | `07` |

The Kassenart itself comes from the KE0 filename prefix at parse time, so the chain is *card IK → KE0 entry → Kassenart filename prefix → KBV code*.

### 2. KTAB (Kostenträgerabrechnungsbereich)

KBV catalogue `S_KTS_KTABRECHNUNGSBEREICH_V1.00`, 11 codes total. For any normal eGK the answer is **always `00` (Primärabrechnung)** — every other code applies to billing schemes that don't involve the eGK at all (BVG/BEG compensation cases, Schwangerschaftsabbruch, Sozialhilfe-Leistungserbringer, asylum-case billing, cross-border worker, etc.). Hardcoded in [internal/egk/form.go:215-225](internal/egk/form.go#L215-L225). The full 11-code reference table is rendered when `--glossary` is set ([cmd/card-reader/glossary.go](cmd/card-reader/glossary.go)).

### 3. WOP (Wohnortprinzip — KV region of residence)

Two-digit KV-region code carried in `EF.AVD` and copied through to GDT field `4131` and to the FHIR `Coverage` extension. The 17 standard codes are listed in [internal/egk/form.go:148-164](internal/egk/form.go#L148-L164):

```
01 Schleswig-Holstein  17 Niedersachsen          51 Rheinland-Pfalz
02 Hamburg             20 Westfalen-Lippe        52 Baden-Württemberg
03 Bremen              38 Nordrhein              71 Bayern
46 Hessen              72 Berlin                 73 Saarland
78 Mecklenburg-Vorpomm. 83 Brandenburg           88 Sachsen-Anhalt
93 Thüringen           98 Sachsen
```

The card stores only the code; the table only exists to annotate the `--table` view with human-readable region names.

### 4. Versichertenart (insured person's status)

`gemSpec_eGK_Fach` Tab. 27. Three valid values:

| Code | Meaning |
| --- | --- |
| `1` | Mitglied (member) |
| `3` | Familienversicherter (family member) |
| `5` | Rentner (pensioner) |

Used to populate GDT `3106`, FHIR `Coverage.gkv-versichertenart`, and the form row.

### 5. KTDA (Kostenträgerdatei) — the only non-trivial reference table

Described in detail above. Single JSON file, one `Entry` per Institutionskennzeichen, ~5,000 active entries. Source files are the SoLE (*Sonstige Leistungserbringer*) variant of the Kostenträgerdatei — they contain the IK→VKNR mapping needed here. The dedicated KBV physician-billing Kostenträgerstammdatei (vendor-distribution only) is *not* used.

### Glossary mode

Run any read with `--glossary` to dump all reference tables under the comprehension table — source-code legend, form-label cross-reference, full KTAB catalogue, acronym list. Off by default.

## What gets auto-filled in the GKV form

The form output is a 21-row table mirroring the standard German GKV billing layout (Tomedo / RED Medical / CGM / etc.). With a fresh `ktda.json`, **16 fields are filled automatically** from card + KTDA + KBV constants. The remaining 5 are practice-level config (BSNR, LANR, fee schedule) or empty by design (no exemption, open-ended cover).

| Field | Source |
| --- | --- |
| Abrechnung | derived (always GKV for an eGK) |
| Kasse | EF.AVD |
| IKNR | EF.AVD (Abrechnender Kostenträger) |
| KTAB | derived → `00` Primärabrechnung |
| Kostenträgergruppe | KTDA (Kassenart → KBV Anlage 6) |
| Karte gelesen | today |
| Versicherungsschutz Beginn / Ende | EF.AVD |
| Besondere Personengruppe | EF.AVD (default `00`) |
| Adresse Teil 1 / 2 | EF.PD |
| Versicherten-Nr. (KVNR) | EF.PD |
| VKNR | KTDA |
| Bedruckungsname | EF.PD (last, first) |
| Versichertenart | EF.AVD |
| WOP | EF.AVD |
| DMP-Kennzeichen | EF.AVD (default `00`) |
| gebührenbefreit bis Datum | EF.GVD |
| Gebührenordnung / Betriebsstätte / Arzt | **manual** — practice config |

## Usage

```sh
card-reader [--input SRC] [--output FMT] [--table | --file] [--glossary]
card-reader ktda <subcommand> [ARGS]
```

### Input source — `--input`

| Value | Effect |
| --- | --- |
| `cardreader` (default) | Read live from a connected PC/SC reader |
| `<path>` to `.gdt` | Parse GDT 2.10 Satzart 6301 |
| `<path>` to `.hl7` | Parse HL7 v2.5 ADT |
| `<path>` to `.fhir.json` | Parse FHIR R4 Bundle (Patient + Coverage) |

File input never touches PC/SC — you can run `card-reader --input file.gdt` on a machine without a reader.

### Output format — `--output`

| Value | Document |
| --- | --- |
| `form` (default for cardreader input) | 21-row GKV billing-form mapping |
| `gdt` | xDT / GDT 2.10 Satzart 6301 — Stammdaten übergeben (ISO-8859-15) |
| `hl7-fhir` | HL7 FHIR R4 Bundle: Patient + Coverage (UTF-8 JSON) |
| `hl7-adt` | HL7 v2.5 ADT^A04 — Register a patient, with PV1 and IN1 (UTF-8) |
| `json` | The form mapping as JSON |

When `--input` is a file and `--output` is omitted, the format defaults to whatever the input file is — so `card-reader --input file.gdt` shows the GDT comprehension view without you repeating yourself.

### Destination — `--table` vs `--file`

- `--table` (default): styled lipgloss "what we understood" table on stdout, rendered in the vocabulary of the chosen `--output`. A comprehension/QA view to verify parsing.
- `-f` / `--file`: write the raw bytes of `--output` to `./output/patient-<KVNR>-<YYYYMMDD-HHMMSS>.<ext>` (basename falls back to `card-<ts>` if the KVNR is missing). Extensions: `.json`, `.fhir.json`, `.hl7`, `.gdt`. `--output form` has no byte form — use `--table`.

### Extras

- `--glossary` — append the source-code / form-label / KTAB / acronym reference tables under the comprehension table. Off by default.
- `-d` / `--debug` — list readers, watch state changes, and dump raw decompressed EF.PD / EF.VD XML. Cardreader input only.
- `EGK_TRACE=1` — log low-level APDU / SFI-fallback chatter to stderr.

## Examples

```sh
# Read card → GKV form table (the default)
card-reader

# Read card → GDT comprehension table
card-reader --output gdt

# Read card → write GDT file to ./output/patient-<KVNR>-<ts>.gdt
card-reader --output gdt --file

# Round-trip: parse a saved GDT file → show GKV form table
card-reader --input file.gdt

# Parse a saved GDT file → show FHIR comprehension table
card-reader --input file.gdt --output hl7-fhir

# Parse a GDT file → write it back out as FHIR JSON
card-reader --input file.gdt --output hl7-fhir --file

# Refresh the insurer table (run quarterly)
card-reader ktda update

# Look up a single IK
card-reader ktda lookup 109519005
```

## Output formats — at a glance

Detailed spec (field maps, planned sub-parameter syntax, variant tables) lives in [docs/output-formats.md](docs/output-formats.md).

- **GDT 2.10** — Satzart 6301 (Stammdaten übergeben), ISO-8859-15, line format `LLL FFFF value \r\n` with byte-length `LLL`. Fields: 8000/8100/9218 header, 0201/0203/0205 sender (placeholders), 3xxx patient block, 4xxx insurance block.
- **HL7 v2.5 ADT^A04** — segments MSH / EVN / PID / PV1 / IN1. UTF-8 (MSH-18). KVNR in PID-3 with assigning authority `GKV&{IKNR}&IKNR`. IKNR in IN1-3. `\r\n` segment terminators (universal-parser-friendly).
- **HL7 FHIR R4** — `Bundle` of `type: collection` with one `Patient` and one `Coverage`. KVNR via `http://fhir.de/sid/gkv/kvid-10`, IKNR via `http://fhir.de/sid/arge-ik/iknr`, de.basisprofil-style extensions for namenszusatz / streetName / houseNumber / versichertenart / besondere-personengruppe / dmp-kennzeichen / wop.
- **JSON** — the 21-row form mapping serialised. Same data, machine-shaped.

Sender / receiver / practice IDs (GDT 0201/0203/0205, HL7 MSH-3..6) ship as placeholders — the deploying practice must override them. Sub-parameter overrides (`--output=hl7-adt=a28,version=2.3` etc.) are designed but not yet wired up; see [docs/output-formats.md](docs/output-formats.md).

## Data sources & update cadence

- **eGK card files**: read live each invocation, no PIN.
  - `EF.PD` (`2F01`) — Persönliche Versichertendaten — name, DOB, address.
  - `EF.VD` (`2F02`) — Allgemeine Versicherungsdaten + Geschützte Versichertendaten — insurer, cover dates, insured type, co-pay status.
  - Both gzipped XML per gematik gemSpec_eGK_Fach v5.2 (ISO-8859-15).
- **KTDA**: 6 KE0 (UN/EDIFACT) files from `gkv-datenaustausch.de`. Quarterly cadence (1.1., 1.4., 1.7., 1.10.) — `ktda update` re-fetches.
- **KTAB**, **Kassenart→KTG**, **WOP**, **Versichertenart**: tiny static tables baked into the binary (KBV / gematik catalogues). Update cadence: rebuild the binary if KBV publishes a new revision (rare).

## Project layout

```
cmd/card-reader/main.go          # entry point — dispatches ktda subcommand or main pipeline
cmd/card-reader/cli.go           # flag parser, --help text, output/extension mapping
cmd/card-reader/cardreader.go    # PC/SC setup, card-vs-file input dispatch, debug dump
cmd/card-reader/ktda_cmd.go      # ktda update / lookup / info subcommands; resolveIK helper
cmd/card-reader/render.go        # lipgloss chrome (title bar) + form-table renderer
cmd/card-reader/glossary.go      # source / form-label / KTAB / acronym reference tables

internal/egk/apdu.go             # PC/SC APDUs (SELECT by AID, READ BINARY by SFI / FID fallback)
internal/egk/egk.go              # high-level Read(card) → CardData (selects DF.HCA, reads PD + VD)
internal/egk/parse.go            # gunzip + XML decode (ISO-8859-15) for PD / AVD / GVD
internal/egk/form.go             # 21-field FormMapping with optional KTDA enrichment

internal/ktda/ke0.go             # KE0 EDIFACT parser (UNB/UNH/IDK/VDT/VKG/NAM/UNT)
internal/ktda/fetch.go           # scrape gkv-datenaustausch index, download KE0 files
internal/ktda/store.go           # merge → ktda.json, lookup, Kassenart→Kostenträgergruppe

internal/document/document.go    # Encoder interface + format registry
internal/document/gdt.go         # GDT 2.10 encoder
internal/document/gdt_parse.go   # GDT 2.10 parser (file → CardData)
internal/document/gdt_table.go   # GDT comprehension-view renderer
internal/document/fhir.go        # FHIR R4 encoder
internal/document/fhir_parse.go  # FHIR R4 parser
internal/document/fhir_table.go  # FHIR comprehension-view renderer
internal/document/hl7v2.go       # HL7 v2.5 ADT^A04 encoder
internal/document/hl7v2_parse.go # HL7 v2 parser
internal/document/hl7v2_table.go # HL7 v2 comprehension-view renderer
internal/document/json.go        # form-mapping JSON encoder
internal/document/*_test.go      # round-trip tests (encode → parse → compare)

internal/output/output.go        # Writer interface — Stdout, File, Multi

cmd/card-probe/main.go           # standalone PC/SC reader-discovery probe (debug aid)

ktda-files/                      # populated by `ktda update` — gitignored
├── raw/                         # downloaded KE0 binaries (6× per quarter)
└── ktda.json                    # compiled, deduplicated lookup table

output/                          # populated by `--file` runs — gitignored
└── patient-<KVNR>-<ts>.<ext>
```

## Limits

- Only the public, no-PIN data is read. PIN-protected applications (NFD, DPE, ePrescription pointers, ePA links) need CV-cert authentication and aren't covered.
- KTAB is a tiny static table (11 codes); no live KBV download.
- KE0 files are the **SoLE** (Sonstige Leistungserbringer) variant. They contain the IK→VKNR mapping we need; the dedicated KBV physician-billing Kostenträgerstammdatei (vendor-distribution only) is not used.
- Sub-parameter syntax (`--output=hl7-adt=a28` etc.) is documented but not implemented yet — flags accept no arguments today.
- Sender / receiver / practice IDs are placeholders in every encoder. Real deployments must override before sending downstream.
- Encoding fidelity: gematik XML declares ISO-8859-15; the decoder honours that explicitly. GDT bytes are written ISO-8859-15; FHIR / HL7 v2 / JSON are UTF-8.
- The KE0 index scrape depends on the gkv-datenaustausch.de page structure. If they redesign the page, the regex in [internal/ktda/fetch.go](internal/ktda/fetch.go) may need updating.

## Troubleshooting

`PRESENT|MUTE` with empty ATR → reader sees a card mechanically but no chip is responding. Causes (in likely order): card upside-down, card in the wrong slot of a multi-slot reader, dirty contacts, or the "reader" is actually an SD-card reader (can't power chip cards). Flip / reseat / clean / use a real CCID reader.

`SELECT EF 2F01 failed: SW=6A82` would mean FID-based EF select isn't supported by the card. The reader uses SFI-based access first and only falls back to FID, so this should not occur on standard eGKs.

XML parse error mentioning `ISO-8859-15` → the charset decoder didn't kick in; rebuild against the latest source.

`--output form has no byte representation` → form is a comprehension view, not a transport format. Pick `gdt` / `hl7-fhir` / `hl7-adt` / `json` for `--file`.

`no KE0 download links found at …` → the gkv-datenaustausch.de index page has changed shape. Inspect the page in a browser, update the regex in [internal/ktda/fetch.go](internal/ktda/fetch.go), and re-run `ktda update`. As a temporary workaround, the parser will accept hand-downloaded KE0 files dropped into `ktda-files/raw/` — re-run `ktda update <other-dir>` to skip the discovery step.

`ktda.json not found … fetching insurer table` → first-run auto-fetch. Subsequent runs use the cached file.

`warning: KTDA files are from Q… — run \`card-reader ktda update\`` → printed when the cached `ktda.json` is from a prior calendar quarter. Non-blocking; run `ktda update` to silence and pick up the new quarter's data.
