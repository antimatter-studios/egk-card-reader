# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release pipeline at [.github/workflows/release.yml](.github/workflows/release.yml)
extracts the section matching the pushed tag (with the leading `v` stripped — so a
`v1.2.0` tag uses the `## [1.2.0]` section here) and publishes it as the GitHub
release body. A tag with no matching section will fail the release job — every
release must have an entry below.

## [1.0.0] — 2026-05-11

### Added

- eGK (elektronische Gesundheitskarte) read pipeline over PC/SC. Parses
  EF.PD (Persönliche Versichertendaten) and EF.VD (Allgemeine + Geschützte
  Versichertendaten) into a unified `CardData` shape.
- Standalone ORGA 9xx CDC-ACM serial transport — talks ISO 7816-3 T=1
  directly to Ingenico/Worldline ORGA 9xx readers without PC/SC. Backends
  for macOS, Linux, and Windows.
- Five output formats:
  - 21-row GKV billing-form comprehension table (default for live reads).
  - GDT 2.10 Satzart 6301 (ISO-8859-15, CR/LF lines).
  - HL7 v2.5 ADT^A04 (MSH/EVN/PID/PV1/IN1, UTF-8).
  - HL7 FHIR R4 Bundle — Patient + Coverage with de.basisprofil
    identifier systems and German GKV extensions.
  - JSON serialisation of the form mapping.
- File-input mode (`--input <path>`) round-trips through every encoder so
  saved `.gdt` / `.hl7` / `.fhir.json` files can be re-rendered or
  re-encoded without a card reader.
- KTDA (Kostenträgerdatei) lookup table — downloads the six SoLE KE0 files
  quarterly from gkv-datenaustausch.de, parses the UN/EDIFACT bodies, and
  compiles a single deduplicated JSON store keyed by IK. Resolves VKNR,
  Kassenart, and Kostenträgergruppe at form-render time.
- `card-probe` companion binary — lists PC/SC readers, prints ATR / state
  flags, surveys known smart-card applications (DF.HCA, DF.ESIGN, DF.HPA,
  DF.AUTO, DF.QES, DF.SMA), and decodes ICCSN.
- `--glossary` flag emits the source-code, form-label, KTAB, and acronym
  reference tables under the comprehension table.

### Tests

- ~90 % statement coverage across the parsing/encoding pipeline; every
  `internal/*` package between 95 % and 100 %.
- Round-trip tests on every encoder–parser pair.
- Fuzz tests on every parser (PD, VD, KE0, GDT, HL7, FHIR).
- Cross-format conversion matrix that pins which `CardData` fields
  survive each format's encode→parse cycle and how lossiness compounds
  across multi-hop chains.
- Documented accepted gaps for PC/SC integration glue and `os.Exit`
  entry points; see [docs/test-plan.md](docs/test-plan.md).

### Infrastructure

- CI test pipeline on every pull request and push to `main`
  (`go vet` + `go test -race`).
- CI release pipeline on semver tag push — native build matrix across
  linux/amd64, linux/arm64, darwin/arm64, windows/amd64; archives plus
  SHA256SUMS uploaded as release artifacts.
