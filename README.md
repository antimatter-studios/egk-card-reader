# card-reader

Reads a German eGK (elektronische Gesundheitskarte) and emits the data in any of four clinical-exchange formats (GKV billing form, GDT 2.10, HL7 v2.5 ADT, HL7 FHIR R4) — or parses one of those formats back from disk and shows what it understood.

Two transports are supported transparently behind a unified driver abstraction:

- **PC/SC** — any standard CCID reader (Cherry ST-2100, OMNIKEY 3121, REINER cyberJack, Identiv uTrust, …) via the OS smart-card stack.
- **ORGA 9xx** (Ingenico/Worldline) — direct over USB-CDC-ACM. No vendor driver needed; the transport is plain ISO 7816-3 T=1 over a virtual serial port. Auto-detected by USB VID/PID `0780:1202`. See [docs/orga-driver/](docs/orga-driver/) for the investigation that led to this implementation.

A `--input` file path also works without any reader at all — same data shape, four input formats parsed.

Read once, render, exit. No TUI, no input loop.

This repository additionally includes a complete implementation of the **gemSpec_COS chapter 13 card-to-card (C2C) handshake** for PIN-protected eGK segments (NFD, DPE, eMP). The crypto and APDU sequencing are in place; end-to-end execution is blocked by hardware availability (see [Reading PIN-protected data](#reading-pin-protected-data) below).

## Requirements

- **A real CCID smart-card reader** (USB), OR an **Ingenico/Worldline ORGA 9xx** terminal. Generic SD/MMC readers are *not* smart-card readers — even if macOS shows them via PC/SC they cannot power the eGK chip. If your card status reads `PRESENT|MUTE` with an empty ATR, that's the symptom.
- **macOS**: PC/SC is built in; ORGA works out of the box via the kernel's `AppleUSBCDCACM` driver — no extra installation.
- **Linux**: `sudo apt install pcscd pcsc-tools libpcsclite-dev` and `sudo systemctl start pcscd` for the PC/SC path; ORGA uses `/sys/bus/usb/devices` directly (no extra package).
- **Windows**: PC/SC built in. ORGA support is currently a stub (TODO: SetupAPI / WMI enumeration — see [pkg/reader/usb/windows.go](pkg/reader/usb/windows.go)).
- File-input mode (`--input <path>`) does not need a reader.

## Build

```sh
go build -o card-reader ./cmd/card-reader   # main binary
go build -o orga-probe  ./cmd/orga-probe    # low-level ORGA probe / debug tool
go build -o card-probe  ./cmd/card-probe    # PC/SC reader-discovery probe
```

## Architecture at a glance

```
┌─────────────────────┐       ┌───────────────────────┐       ┌──────────────────────┐
│  Input acquisition  │       │  Enrichment           │       │  Rendering           │
│                     │       │                       │       │                      │
│  ┌───────────────┐  │       │  IKNR ──┐             │       │  ┌────────────────┐  │
│  │ ORGA / PC/SC  │──┼──┐    │         ▼             │       │  │ comprehension  │  │
│  │ (autodetect)  │  │  │    │    ktda.json          │       │  │ table (stdout) │  │
│  └───────────────┘  │  │    │  ┌─────────────────┐  │       │  └────────────────┘  │
│  ┌───────────────┐  │  │    │  │ IK → Name,      │  │       │  ┌────────────────┐  │
│  │ .gdt file     │──┼──┤    │  │      VKNR,      │  │       │  │ raw-bytes file │  │
│  └───────────────┘  │  ├──▶ │  │      Kassenart, │ ─┼──┬──▶ │  │ ./output/...   │  │
│  ┌───────────────┐  │  │    │  │      validity,  │  │  │    │  └────────────────┘  │
│  │ .hl7 file     │──┼──┤    │  │      links      │  │  │    └──────────────────────┘
│  └───────────────┘  │  │    │  └─────────────────┘  │  │
│  ┌───────────────┐  │  │    │         │             │  │
│  │ .fhir.json    │──┼──┘    │         ▼             │  │
│  └───────────────┘  │       │   IKInfo {Name,VKNR,  │  │
└─────────────────────┘       │    Kassenart,KTG}     │  │
                              └───────────┬───────────┘  │
                                          ▼              │
                                   CardData + IKInfo ────┘
                                   (one shape, four renderers)
```

### Reader-driver layering

The "ORGA / PC/SC (autodetect)" box hides a small abstraction:

```
cmd/card-reader, cmd/orga-probe
            ↓
pkg/reader    ← Session + Card interfaces, Probe factory, DeviceInfo
            ↓
   ┌────────┴────────┐
   ↓                 ↓
pkg/reader/    pkg/reader/
  generic             orga
   ↓                 ↓
PC/SC (CCID)        T=1 over CDC-ACM
ebfe/scard          (own implementation, ~250 LoC)
   ↓                 ↓                ↓
Cherry, OMNIKEY,    ORGA 930 M ──→  pkg/reader/usb
REINER cyberJack,                     (darwin: ioreg,
Identiv uTrust, …                      linux:  /sys/bus/usb,
                                       windows: stub)
```

- `pkg/reader.Card` is the minimal contract — `Transmit(apdu []byte) ([]byte, error)`. Both `*orga.Slot` and `*generic.Card` satisfy it structurally; the eGK reader code in `pkg/egk` doesn't know which is underneath.
- `reader.Detect()` probes the system (USB VID/PID match for ORGA via the OS-specific `pkg/reader/usb/` layer, then PC/SC daemon listing) and returns the best-priority driver.
- `Session.Identify() DeviceInfo` returns manufacturer / product / serial / device path / VID-PID / firmware / selection reason — every read prints these to stderr so you can see which device was chosen and why.

Full architecture write-up: [docs/reader-architecture.md](docs/reader-architecture.md).

1. **Acquire** — read live over PC/SC, or parse a previously written `.gdt` / `.hl7` / `.fhir.json`. Both paths land in the same internal `CardData` shape (see [pkg/egk/egk.go](pkg/egk/egk.go)).
2. **Enrich** — the eGK carries the insurer's `IKNR` and display name, but not the `VKNR`, `Kassenart`, or `Kostenträgergruppe` that German practice-management forms demand. Those come from `ktda.json`, a compiled lookup table derived from six quarterly KE0 files (see [KTDA — the insurer lookup table](#ktda--the-insurer-lookup-table)).
3. **Render** — the same `CardData + IKInfo` is fed to one of five encoders (`form`, `gdt`, `hl7-fhir`, `hl7-adt`, `json`). Output goes to a styled lipgloss "comprehension" table on stdout (verify what was parsed), or to `./output/patient-<KVNR>-<timestamp>.<ext>` as raw bytes.

The `Encoder` and `Writer` interfaces ([pkg/document/document.go](pkg/document/document.go), [pkg/output/output.go](pkg/output/output.go)) keep *what* the document looks like decoupled from *where* it goes — adding a new format is one Encoder and one registry line.

## How the eGK read pipeline works

The eGK is a Java-Card chip card. The `pkg/egk` package talks to it via raw ISO 7816-4 APDUs over whichever transport the reader factory picked (PC/SC or ORGA T=1).

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

- **APDUs** ([pkg/egk/apdu.go](pkg/egk/apdu.go)) — ISO 7816-4 commands. `SELECT AID` switches into the Health Card Application; `READ BINARY` pulls the file body. The reader prefers SFI-based access (one APDU = SELECT + READ in a single shot) and only falls back to FID-based `SELECT EF` when SFI fails. SFI mode caps the file offset at 255, so once the read passes that boundary the implementation drops back to plain READ BINARY against the implicit current EF (see [pkg/egk/apdu.go:180-211](pkg/egk/apdu.go#L180-L211)).
- **Files** ([pkg/egk/egk.go](pkg/egk/egk.go)) — only two are read, both unprotected by PIN:
  - `EF.PD` (`2F01`) — *Persönliche Versichertendaten* — name, DOB, gender, address.
  - `EF.VD` (`2F02`) — packs *AVD* (Allgemeine Versicherungsdaten) + *GVD* (Geschützte Versichertendaten). The first 8 bytes are four big-endian pointers (`AVD-start, AVD-end, GVD-start, GVD-end`) into the rest of the file; each section is independently gzipped XML.
- **Decompress + parse** ([pkg/egk/parse.go](pkg/egk/parse.go)) — gunzip, then `encoding/xml` with a `CharsetReader` that resolves `ISO-8859-15` (the gematik-declared charset) via `golang.org/x/text/encoding/charmap`. The schemas are gematik `gemSpec_eGK_Fach` v5.2 — `UC_PersoenlicheVersichertendatenXML`, `UC_AllgemeineVersicherungsdatenXML`, `UC_GeschuetzteVersichertendatenXML`.

### Additional reads beyond EF.PD / EF.VD

Around the main public-data pipeline above, the reader also pulls four diagnostic / identification artefacts:

- **EF.GDO at MF** ([pkg/egk/mf.go](pkg/egk/mf.go)) — 10-byte **ICCSN** (Integrated Circuit Card Serial Number, BER-TLV tag `5A`). Card identity, never PIN-protected. Surfaced as a `--glossary` diagnostic row.
- **EF.Version2 at MF** ([pkg/egk/mf.go](pkg/egk/mf.go)) — G2 card version tags (chip type, object-system version, application versions). Useful for telling G2 cards apart from G1 in the same fleet.
- **EF.StatusVD inside DF.HCA** ([pkg/egk/status.go](pkg/egk/status.go)) — insurance-data freshness markers (last-update timestamp, validity status). Surfaced alongside the parsed insurance data so practices can spot stale cards.
- **DF.ESIGN cardholder X.509 certs** ([pkg/egk/esign.go](pkg/egk/esign.go)) — the two publicly readable cert slots: `FID C500` (RSA-2048 cardholder authentication) and `FID C504` (ECDSA on brainpoolP256r1 — Go stdlib doesn't decode the curve, so the parser falls back to a tolerant ASN.1 walk that pulls Subject / Issuer / Validity / signature-alg OID directly out of the TBSCertificate). The remaining `C500..C50F` slots are CV-certs gated by PIN / C2C and are deliberately skipped here.

All four are best-effort — a card that doesn't expose any of them just yields a `nil` field in `CardData`, and the rest of the read proceeds.

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
        DiscoverFiles()                  pkg/ktda/fetch.go
               │
               │  6 KE0 URLs (AO/EK/BK/IK/BN/LK + verfahren 05 + Qq + yy)
               ▼
        Download(urls, dir)              pkg/ktda/fetch.go
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
        Parse(r, kassenart)              pkg/ktda/ke0.go
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
        Compile(allEntries, sources)     pkg/ktda/store.go
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

[pkg/ktda/fetch.go](pkg/ktda/fetch.go) is intentionally minimal — no HTML parser dependency:

1. **`DiscoverFiles()`** — `GET https://www.gkv-datenaustausch.de/leistungserbringer/sonstige_leistungserbringer/kostentraegerdateien_sle/kostentraegerdateien.jsp`, then a regex pass over the body for `href="…\.ke0"`. Two regexes: one to pull every `.ke0` href out of the HTML, one to filter the basenames against `(AO|EK|BK|IK|BN|LK)\d{2}Q\d\d{2}\.ke0`. Hardcoded filenames would rot every quarter — scraping the index keeps the tool current as long as the page structure holds.
2. **`Download(urls, dir)`** — sequential `GET`s into `ktda-files/raw/`, each via `<dst>.tmp` + atomic rename so a crashed download doesn't leave a half-file in place.
3. **`Parse(r, kassenart)`** — see below.
4. **`Compile(allEntries, sources)`** — merge into a single `Store` keyed by IK, write pretty-printed JSON to `ktda-files/ktda.json`.

### KE0 — what's in the wire format

KE0 is UN/EDIFACT. Charset is ISO-8859-1. Segments end with `'` (apostrophe), fields are separated by `+`, sub-fields by `:`. A `?` is the EDIFACT release character — `?+` is a literal `+`. The parser at [pkg/ktda/ke0.go](pkg/ktda/ke0.go) handles only the segments we need:

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

KBV Anlage 6 BMV-Ä. Hardcoded in [pkg/ktda/store.go:108-124](pkg/ktda/store.go#L108-L124):

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

KBV catalogue `S_KTS_KTABRECHNUNGSBEREICH_V1.00`, 11 codes total. For any normal eGK the answer is **always `00` (Primärabrechnung)** — every other code applies to billing schemes that don't involve the eGK at all (BVG/BEG compensation cases, Schwangerschaftsabbruch, Sozialhilfe-Leistungserbringer, asylum-case billing, cross-border worker, etc.). Hardcoded in [pkg/egk/form.go:215-225](pkg/egk/form.go#L215-L225). The full 11-code reference table is rendered when `--glossary` is set ([cmd/card-reader/glossary.go](cmd/card-reader/glossary.go)).

### 3. WOP (Wohnortprinzip — KV region of residence)

Two-digit KV-region code carried in `EF.AVD` and copied through to GDT field `4131` and to the FHIR `Coverage` extension. The 17 standard codes are listed in [pkg/egk/form.go:148-164](pkg/egk/form.go#L148-L164):

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
| `cardreader` (default) | Auto-detect — ORGA (USB VID/PID `0780:1202`) wins, else PC/SC |
| `orga` | Force the ORGA driver |
| `orga:/dev/cu.usbmodemXXX` | Force the ORGA driver on a specific serial device |
| `pcsc` or `generic` | Force the PC/SC driver |
| `<path>` to `.gdt` | Parse GDT 2.10 Satzart 6301 |
| `<path>` to `.hl7` | Parse HL7 v2.5 ADT |
| `<path>` to `.fhir.json` | Parse FHIR R4 Bundle (Patient + Coverage) |

File input never touches any reader — you can run `card-reader --input file.gdt` on a machine without one.

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
- `EGK_TRACE=1` — log high-level APDU / SFI-fallback chatter to stderr.
- `ORGA_TRACE=1` — log every T=1 block sent/received by the ORGA driver, with millisecond timestamp + PCB classification.

### Companion binaries

```sh
# Low-level ORGA probe / debug tool — never used in normal billing flow,
# but indispensable for investigating new cards or terminal firmware:
orga-probe -info                    # CT-BCS terminal info
orga-probe -status 1                # slot status (1 = front / eGK, 2 = back / SMC)
orga-probe -activate 1              # power up slot, print ATR
orga-probe -identify 2              # full identity probe — emits structured markdown
orga-probe -slot 1 -apdu "00 A4 00 0C 02 3F 00"
                                    # send a raw APDU to a slot (read-only by default;
                                    # VERIFY / UPDATE / ERASE refused unless
                                    # -UNSAFE-allow-pin-write is set)
orga-probe -readcert 2 -aid "A0 00 00 01 67 45 53 49 47 4E" -fid "C5 00" -out cert.der
                                    # extract an X.509 cert from a card EF + parse
orga-probe -c2c 2 -c2c-test-roots=true
                                    # drive C2C Discover + Validate phases against
                                    # the card in slot 2, emit a structured report

# PC/SC reader-discovery probe (pre-dates the orga work):
card-probe                          # list connected readers + status flags
```

See [cmd/orga-probe/](cmd/orga-probe/) for the full flag set and
[docs/orga-driver/](docs/orga-driver/) for the wire-protocol investigation that
produced the driver.

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

# Force the ORGA driver (skip PC/SC even if a CCID reader is also plugged in)
card-reader --input orga --output json --file

# Trace every T=1 block to stderr (debugging an ORGA terminal issue)
ORGA_TRACE=1 card-reader --output json --file
```

## Reading PIN-protected data

The eGK carries two classes of data:

- **Public** — `EF.PD` and `EF.VD` (insurance master data + billing). No PIN, no authentication. This is what the default read pipeline handles, and what every German practice's billing workflow uses.
- **PIN-protected** — `EF.NFD` (Notfalldaten), `EF.DPE` (persönliche Erklärungen), `EF.eMP` (Medikationsplan), and the eRezept / ePA pointers. These require a card-to-card (C2C) handshake between the eGK and an authenticating SMC-B, after which the cardholder's PIN unlocks the segment.

This repository contains a complete implementation of the gemSpec_COS chapter 13 C2C handshake under [internal/c2c/](internal/c2c/). Five phases are implemented and unit-tested:

| Phase | Purpose | File |
| --- | --- | --- |
| 1. DiscoverPeerCerts | Read SMC-B CV-cert chain off the card by FID sweep | [discover.go](internal/c2c/discover.go) |
| 2. ValidatePeerChain | Verify chain locally against the embedded gematik CVC-Roots | [handshake.go](internal/c2c/handshake.go), [keys/](internal/c2c/keys/) |
| 3. PresentToVerifier | Push the chain into the eGK via MSE SET DST + PSO VERIFY CERTIFICATE | [phase_present.go](internal/c2c/phase_present.go) |
| 4. MutualAuthenticate | GET CHALLENGE on eGK → INTERNAL AUTHENTICATE on SMC-B → EXTERNAL AUTHENTICATE on eGK | [phase_mutual.go](internal/c2c/phase_mutual.go) |
| 5. OpenSecureChannel | Derive AES K_ENC / K_MAC / SSC → `*sm.Session` for subsequent SM-protected APDUs (VERIFY PIN, READ BINARY) | [phase_secure.go](internal/c2c/phase_secure.go) |

Supporting subpackages:

| Subpackage | Purpose |
| --- | --- |
| [internal/c2c/cvcert/](internal/c2c/cvcert/) | BER-TLV parser for BSI TR-03110 / gemSpec_PKI CV-certs (tag 7F21 / 7F4E / 5F37) |
| [internal/c2c/brainpool/](internal/c2c/brainpool/) | Brainpool P-256r1 / P-384r1 / P-512r1 curve arithmetic + ECDSA verify (Go stdlib has no Brainpool) |
| [internal/c2c/keys/](internal/c2c/keys/) | gematik X.509 TSL roots + 4 embedded CVC-Roots (DEZGW870226 production active + DEGXX890225/880224/870222 test) + chain validation |
| [internal/c2c/sm/](internal/c2c/sm/) | gemSpec_COS §10 Secure Messaging Wrap/Unwrap + AES-CMAC per NIST SP 800-38B |

### The four walls

PIN-protected reads require **all four** to be true simultaneously:

1. **Driver** — the host can talk to the terminal. ✅ Done — ORGA driver works on macOS / Linux.
2. **SMC-B with valid CV-certs** — a partner card carrying a chain to gematik's production CVC-Root. ❌ Out of reach without registration as a Leistungserbringer (healthcare provider with KV registration).
3. **Cardholder PIN** — issued by the Krankenkasse separately from the card (request via TK-App / VideoIdent / Geschäftsstelle; arrives by Einschreiben). 🟡 In hand or obtainable, but using it is blocked behind a mechanical safety guard until Wall 2 is satisfied — a wrong VERIFY decrements an on-card counter that never decays, three strikes locks the PIN, and only the PUK can reset.
4. **gemSpec_COS C2C in code** — implementation of phases 1-5. ✅ Done — 404 tests across the c2c packages.

If Walls 1, 3, and 4 are satisfied but Wall 2 is not (the current state), the code is unable to read PIN-protected data — the handshake fails at phase 3 with `SW=6300` because the eGK rejects a cert that doesn't chain to its on-card production CVC-Root. The PIN itself is never sent.

**Full reading order**:

- [docs/c2c/README.md](docs/c2c/README.md) — package overview, status table, live-probe instructions
- [docs/c2c/walls.md](docs/c2c/walls.md) — the four-walls model: criteria, current state, evidence per wall
- [docs/c2c/pin-workflow.md](docs/c2c/pin-workflow.md) — PIN provenance (Krankenkasse), counter math, when VERIFY will actually be issued
- [docs/c2c/cvc-root-research.md](docs/c2c/cvc-root-research.md) — provenance log for all 15 gematik CVC-Root certs, current active anchors, source URLs and fingerprints
- [docs/c2c/slot2-no-cvcerts.md](docs/c2c/slot2-no-cvcerts.md) — finding that the project's reference test SMC-B doesn't carry CV-certs (additional wall)
- [docs/c2c/plan.md](docs/c2c/plan.md) — the original implementation plan (now mostly retrospective)

### Mechanical safety guard

The ORGA driver refuses dangerous APDUs at `Slot.Transmit` time unless `Options.AllowPINWrite=true` (or the `-UNSAFE-allow-pin-write` CLI flag). The list ([pkg/reader/orga/safety.go](pkg/reader/orga/safety.go)) covers:

- ISO 7816: `0x20` VERIFY, `0x24` CHANGE REFERENCE DATA, `0x26`/`0x28` DISABLE/ENABLE VERIFICATION REQUIREMENT, `0x2C` RESET RETRY COUNTER, `0xD6` UPDATE BINARY, `0xDC` UPDATE RECORD, `0xDA` PUT DATA, `0xE0`/`0x0E` ERASE BINARY, `0xEE` ERASE RECORD
- CT-BCS: `0x16` INPUT, `0x18` PERFORM VERIFICATION, `0x19` MODIFY VERIFICATION DATA (any of which can drive the terminal pinpad and trigger a VERIFY on the card)

The block exists so a hex-byte typo in a probe session can never burn a PIN attempt. A wrong VERIFY decrements the on-card retry counter atomically inside the secure element; the counter doesn't decay over time; three strikes blocks the PIN. The fourth and tenth wrong PUKs lock the card permanently for protected reads.

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
cmd/
├── card-reader/main.go              # entry point — dispatches ktda subcommand or main pipeline
├── card-reader/cli.go               # flag parser, --help text, output/extension mapping
├── card-reader/cardreader.go        # input dispatch (auto / orga / pcsc / file), Identify output
├── card-reader/ktda_cmd.go          # ktda update / lookup / info subcommands; resolveIK helper
├── card-reader/render.go            # lipgloss chrome (title bar) + form-table + diagnostics
├── card-reader/glossary.go          # source / form-label / KTAB / acronym reference tables
│
├── card-probe/main.go               # PC/SC reader-discovery probe (debug aid)
│
└── orga-probe/                      # low-level ORGA debug tool
    ├── main.go                      # flag parsing, command dispatch
    ├── identify.go                  # -identify <slot>: structured card-identity dump
    ├── readcert.go                  # -readcert: extract + parse X.509 from a card EF
    ├── c2c.go                       # -c2c <slot>: drive C2C Discover + Validate phases
    ├── atr.go                       # ATR decoder (TS / T0 / TA1 / TBn / TCn / TDn / TCK)
    └── tlv.go                       # BER-TLV decoder for EF.ATR vendor records

pkg/reader/                     # reader-driver abstraction
├── reader.go                        # Card + Session interfaces, Options, Open() factory
├── probe.go                         # Probe / Detect / Driver / OpenDriver
├── generic/                         # PC/SC driver (Cherry, OMNIKEY, cyberJack, …)
├── orga/                            # ORGA 9xx driver (T=1 over CDC-ACM, ~250 LoC)
│   ├── orga.go                      # Terminal type, Open, Close, slot routing
│   ├── transport.go                 # T=1 block build/parse, LRC EDC
│   ├── exchange.go                  # transactWithNAD, RESYNCH, IFS/WTX auto-ack, chaining
│   ├── ctbcs.go                     # CT-BCS helpers (RESET, REQUEST_ICC, GET_STATUS, EJECT)
│   ├── safety.go                    # mechanical block on VERIFY / UPDATE / ERASE / etc.
│   ├── errors.go                    # friendly mapping for ENXIO / ENOENT / EACCES / EBUSY
│   ├── trace.go                     # ORGA_TRACE=1 hooks — timestamped T=1 block log
│   └── serial_darwin.go             # termios open at 9600 8N1 (darwin only)
└── usb/                             # cross-OS USB enumeration
    ├── usb.go                       # Probe interface, Device struct, Default(), ErrUnsupported
    ├── darwin.go                    # parse `ioreg -r -c IOUSBHostDevice -l -w0`
    ├── linux.go                     # read /sys/bus/usb/devices/ directly (no lsusb / libusb)
    ├── windows.go                   # stub returning ErrUnsupported (TODO SetupAPI)
    └── other.go                     # catch-all stub for other GOOS

internal/c2c/                        # gemSpec_COS chapter 13 card-to-card authentication
├── doc.go + handshake.go            # 5-phase orchestrator, typed *c2c.Error, Session()
├── discover.go                      # CV-cert FID sweep on a card (DF.SMA / DF.ESIGN fallback)
├── phase_present.go                 # MSE SET DST + PSO VERIFY CERTIFICATE (phase 3)
├── phase_mutual.go                  # GET CHALLENGE / INT-AUTH / EXT-AUTH (phase 4)
├── phase_secure.go                  # AES-128 KDF (gemSpec_Krypt Algorithm-2) → *sm.Session (phase 5)
├── cvcert/                          # BSI TR-03110 / gemSpec_PKI CV-cert ASN.1 parser
├── brainpool/                       # Brainpool P-256r1 / P-384r1 / P-512r1 + ECDSA verify
├── keys/                            # gematik X.509 TSL roots + embedded CVC-Roots + chain verify
└── sm/                              # gemSpec_COS §10 Secure Messaging + AES-CMAC

pkg/egk/
├── card.go                          # Card interface (Transmit) — implemented by both transports
├── constants.go                     # gematik AIDs, FIDs, SFIs, INS codes — named, not magic
├── apdu.go                          # SELECT by AID, READ BINARY by SFI / FID fallback
├── egk.go                           # high-level Read(card) → CardData
├── parse.go                         # gunzip + XML decode (ISO-8859-15) for PD / AVD / GVD
├── mf.go                            # MF-level reads: EF.GDO (ICCSN) + EF.Version2
├── status.go                        # EF.StatusVD inside DF.HCA — VD freshness markers
├── esign.go                         # DF.ESIGN cardholder X.509 certs (RSA + brainpool fallback)
├── probe.go                         # APDU sweep — surveys known AIDs/EFs (used by card-probe)
└── form.go                          # FormMapping + diagnostic rows with optional KTDA enrichment

pkg/ktda/
├── ke0.go                           # KE0 EDIFACT parser (UNB/UNH/IDK/VDT/VKG/NAM/UNT)
├── fetch.go                         # scrape gkv-datenaustausch index, download KE0 files
└── store.go                         # merge → ktda.json, lookup, Kassenart→Kostenträgergruppe

pkg/document/
├── document.go                      # Encoder interface + format registry
├── gdt.{go,_parse.go,_table.go}     # GDT 2.10 (Satzart 6301) encode + parse + comprehension view
├── fhir.{go,_parse.go,_table.go}    # HL7 FHIR R4 (Patient + Coverage Bundle) encode + parse + view
├── hl7v2.{go,_parse.go,_table.go}   # HL7 v2.5 ADT^A04 encode + parse + view
├── json.go                          # form-mapping JSON encoder
└── *_test.go                        # round-trip tests (encode → parse → compare)

pkg/output/output.go            # Writer interface — Stdout, File, Multi

docs/
├── reader-architecture.md           # reader-driver layering + USB-enumeration matrix
├── c2c/                             # gemSpec_COS C2C handshake docs (5 markdown pages)
├── orga-driver/                     # ORGA wire-protocol investigation (7 numbered pages + cards/)
├── output-formats.md                # detailed format specs (field maps, sub-parameter plans)
└── test-plan.md

ktda-files/                          # populated by `ktda update` — gitignored
├── raw/                             # downloaded KE0 binaries (6× per quarter)
└── ktda.json                        # compiled, deduplicated lookup table

output/                              # populated by `--file` runs — gitignored
└── patient-<KVNR>-<ts>.<ext>
```

## What's implemented vs not

### Implemented (working today)

- **Public eGK read pipeline** over PC/SC or ORGA — `EF.PD` + `EF.VD`, gunzip + ISO-8859-15 XML decode, full GKV form mapping with KTDA enrichment.
- **Diagnostic & identification reads** — `EF.GDO` (ICCSN), `EF.Version2` (G2 card version tags), `EF.StatusVD` inside `DF.HCA`, and `DF.ESIGN` cardholder X.509 certs (RSA-2048 + brainpoolP256r1 via tolerant ASN.1 walk). Surfaced as `--glossary` rows; all best-effort.
- **Format conversion** — GDT 2.10 / HL7 v2.5 ADT^A04 / HL7 FHIR R4 / JSON, all bidirectional (encode + parse + comprehension table). Round-trip tested.
- **ORGA 9xx driver on macOS + Linux** — ISO 7816-3 T=1 over USB-CDC-ACM. USB VID/PID detection (no false positives from generic CDC-ACM devices). RESYNCH, IFS / WTX auto-ack, chained I-blocks. Recovery procedure for "Fatal Error 3 / SW=64A2" documented and tested.
- **Reader-driver abstraction** — `Session` + `Card` interfaces, factory with autodetect, `Identify() DeviceInfo` for self-describing transports. Cross-OS USB probe (macOS ioreg, Linux sysfs, Windows stub, fallback stub).
- **gemSpec_COS C2C handshake — all 5 phases** — code-complete and unit-tested. CV-cert BER-TLV parser, Brainpool P-256/384/512r1 ECC, gemSpec_COS Secure Messaging (AES-CBC + AES-CMAC), gematik CVC-Root + X.509 TSL trust anchors embedded.
- **Mechanical safety guard** — PIN-counter-decrementing APDUs (VERIFY, CHANGE REFERENCE DATA, RESET RETRY COUNTER) refused at the driver layer unless explicitly overridden.
- **Tracing** — `ORGA_TRACE=1` for T=1 block log; `EGK_TRACE=1` for APDU / SFI-fallback log.
- **KTDA quarterly refresh** — scrape `gkv-datenaustausch.de`, download 6 KE0 files, parse EDIFACT, merge, store as `ktda.json`.
- **Companion debug tools** — `orga-probe -identify`, `-readcert`, `-c2c`, plus the older `card-probe` for PC/SC.
- **Hardware-free test infrastructure** — scriptable `fakeSerialIO` ([pkg/reader/orga/mock_test.go](pkg/reader/orga/mock_test.go)) lets the orga T=1 transport, CT-BCS layer, exchange state machine, and safety guard be unit-tested without a physical terminal. Overall statement coverage **73.6 %** (orga 83.6 %, reader 62.1 %, c2c 83.8 %, egk 83.9 %, all parsing/encoding packages 88–100 %). Remaining gaps are hardware-bound entry points (`orga.Open`, `openSerial`, `generic.Open`) — see [docs/test-plan.md](docs/test-plan.md).

### Not yet implemented

- **PIN-protected eGK reads (NFD / DPE / eMP)** — the code path is complete; the live execution is blocked by **Wall 2** (no production SMC-B available). Available test SMC-B doesn't carry CV-certs anyway. See [docs/c2c/walls.md](docs/c2c/walls.md).
- **Windows ORGA support** — `pkg/reader/usb/windows.go` is a stub returning `ErrUnsupported`. TODO: SetupAPI (`SetupDiGetClassDevs`) or WMI (`Get-PnpDevice`) enumeration to match the macOS/Linux behaviour.
- **Older inactive gematik CVC-Root predecessors** (1st-6th production generations, `DEZGW810214`…`DEZGW860224`) not yet embedded — current active root + 3 test gens are in. Add when X/Y coordinates can be extracted directly from the source DERs.
- **External Brainpool ECDSA KAT** — current ECDSA verification is consistency-tested with self-derived signatures; an externally-vetted vector (BSI TR-03111 / wycheproof) should be added before production reliance on Brainpool signatures.
- **Sub-parameter syntax** — `--output=hl7-adt=a28,version=2.3` etc. is designed but not wired up; flags accept no arguments today. See [docs/output-formats.md](docs/output-formats.md).
- **Real sender / receiver / practice IDs** — GDT 0201/0203/0205, HL7 MSH-3..6, FHIR `MessageHeader.source` ship as placeholders. Real deployments must override before sending downstream.
- **SMC-B C2C direction 2** — only Direction 1 of the mutual auth is wired (eGK challenges SMC-B). The reverse (SMC-B challenges eGK) isn't needed for unlocking eGK protected reads but would be needed for full symmetric SM.
- **KE0 variant** — files used are the **SoLE** (Sonstige Leistungserbringer) variant. They contain the IK→VKNR mapping we need; the dedicated KBV physician-billing Kostenträgerstammdatei (vendor-distribution only) is not used.

### Hardware caveats

- The ORGA driver has been live-tested only on macOS against an Ingenico ORGA 930 M with firmware `V5.03 7.05`. The implementation per spec should cover the whole 9xx family but cross-firmware validation hasn't happened.
- The KE0 index scrape depends on the `gkv-datenaustausch.de` page structure. If they redesign the page, the regex in [pkg/ktda/fetch.go](pkg/ktda/fetch.go) may need updating.
- Encoding fidelity: gematik XML declares ISO-8859-15; the decoder honours that explicitly. GDT bytes are written ISO-8859-15; FHIR / HL7 v2 / JSON are UTF-8.
- KTAB is a tiny static table (11 codes); no live KBV download.

## Troubleshooting

### Reader detection

`reader/orga: no ORGA terminal detected (VID 0x0780 / PID 0x1202)` → the USB probe found no matching device. Check `ioreg -r -c IOUSBHostDevice -l -w0 | grep ORGA` (macOS) or `lsusb | grep 0780` (Linux); if absent, the terminal isn't enumerated — check the cable / power / DFU mode (PID `0xDF55` instead of `0x1202`).

`PRESENT|MUTE` with empty ATR (PC/SC) → reader sees a card mechanically but no chip is responding. Causes (in likely order): card upside-down, card in the wrong slot of a multi-slot reader, dirty contacts, or the "reader" is actually an SD-card reader (can't power chip cards). Flip / reseat / clean / use a real CCID reader.

`PC/SC: no readers found` → on Linux, ensure `pcscd` is running (`sudo systemctl start pcscd`).

### ORGA terminal in a bad state

`device not configured` (ENXIO) → the kernel sees the `/dev/cu.usbmodem*` node but the USB endpoint isn't responding. Usually means the terminal is mid-reboot. Wait ~5 seconds; the driver maps the errno to a friendly message with recovery hints. If it persists, unplug + replug the USB cable.

Every APDU to slot 2 returns `SW=64A2` (vendor-specific "card not in operational state") → the slot-2 card is stuck after a terminal reset. Recover by power-cycling the slot with CT-BCS REQUEST ICC P2=01:

```sh
./orga-probe -slot 0 -apdu "20 12 02 01 00"   # returns ATR + SW=6201
./orga-probe -slot 2 -apdu "00 A4 00 0C 02 3F 00"   # SELECT MF now works (9000)
```

Full incident write-up: [docs/orga-driver/07-card-recovery.md](docs/orga-driver/07-card-recovery.md).

### Other

`SELECT EF 2F01 failed: SW=6A82` would mean FID-based EF select isn't supported by the card. The reader uses SFI-based access first and only falls back to FID, so this should not occur on standard eGKs.

XML parse error mentioning `ISO-8859-15` → the charset decoder didn't kick in; rebuild against the latest source.

`--output form has no byte representation` → form is a comprehension view, not a transport format. Pick `gdt` / `hl7-fhir` / `hl7-adt` / `json` for `--file`.

`no KE0 download links found at …` → the gkv-datenaustausch.de index page has changed shape. Inspect the page in a browser, update the regex in [pkg/ktda/fetch.go](pkg/ktda/fetch.go), and re-run `ktda update`. As a temporary workaround, the parser will accept hand-downloaded KE0 files dropped into `ktda-files/raw/` — re-run `ktda update <other-dir>` to skip the discovery step.

`ktda.json not found … fetching insurer table` → first-run auto-fetch. Subsequent runs use the cached file.

`warning: KTDA files are from Q… — run \`card-reader ktda update\`` → printed when the cached `ktda.json` is from a prior calendar quarter. Non-blocking; run `ktda update` to silence and pick up the new quarter's data.

`REFUSED INS=0x20 — VERIFY …` → the safety guard blocked an APDU that would decrement the card's PIN retry counter. Intentional; pass `-UNSAFE-allow-pin-write` (CLI) or `Options.AllowPINWrite=true` (library) only when you really mean it. See [docs/c2c/pin-workflow.md](docs/c2c/pin-workflow.md).

`c2c: present-to-verifier: …: SW=6300 authentication failed` → expected when running phase 3 with a CV-cert chain that doesn't match the eGK's on-card production CVC-Root. This is Wall 2 of the four-walls model — the test SMC-B's chain is wrong/expired/missing. See [docs/c2c/walls.md](docs/c2c/walls.md).
