# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release pipeline at [.github/workflows/release.yml](.github/workflows/release.yml)
extracts the section matching the pushed tag (with the leading `v` stripped — so a
`v1.2.0` tag uses the `## [1.2.0]` section here) and publishes it as the GitHub
release body. A tag with no matching section will fail the release job — every
release must have an entry below.

## [1.1.0] — 2026-05-11

### Added

- C2C handshake — full gemSpec_COS chapter 13 card-to-card mutual
  authentication implementation. Covers peer-cert discovery
  (`internal/c2c/discover.go`), CV-cert chain validation against gematik
  Brainpool P256 roots (`internal/c2c/keys/cvc_roots.go`), the
  PresentToVerifier / MutualAuthenticate / OpenSecureChannel phases, and
  Secure Messaging session-key derivation. Crypto and APDU sequencing are
  complete; end-to-end execution is gated on hardware availability (live
  SMC-B + cardholder PIN + production gematik CV-certs).
- USB descriptor probe subpackage (`internal/reader/usb`) — replaces the
  best-effort `/dev/cu.usbmodem*` glob with VID/PID matching. darwin
  backend via `ioreg`, linux via `/sys/bus/usb`, windows stub. Returns
  full descriptor metadata (manufacturer, product, serial, device path).
- Reader hardware identification — `Session.Identify()` returns a
  `DeviceInfo` struct (manufacturer, product, serial, device path, USB
  IDs, firmware string, selection reason) populated from whichever
  transport is in use. Every live read prints the chosen reader's
  identifying info to stderr.
- DF.ESIGN cardholder cert reading — SELECT DF.ESIGN and pull the two
  publicly readable X.509 certs (FID C500 RSA-2048, FID C504 ECDSA on
  brainpoolP256r1). Parse path falls back to a tolerant ASN.1 walk when
  `crypto/x509` rejects brainpool curves, so Subject / Issuer / Validity
  / signature-alg OID always come through.
- MF-level diagnostic reads — EF.GDO (ICCSN), EF.Version2 (G2 card
  version tags), EF.StatusVD inside DF.HCA (insurance-data freshness).
  Surfaced as `--glossary` diagnostic rows.
- GVD selective-contract fields in form mapping (WOP, Versichertenart,
  KTAB, Selektivverträge).
- `cmd/orga-probe` — low-level ORGA debug binary with `identify`, `atr`,
  `tlv`, `readcert`, and `c2c` sub-commands for diagnosing terminal /
  card issues without going through the full card-reader pipeline.
- ORGA T=1 trace logging — `ORGA_TRACE=1` logs every block sent /
  received with timestamp and PCB classification for post-mortem
  analysis.
- Friendlier ORGA error wrapping — actionable messages for macOS ENXIO
  (device just rebooted, wait ~5 s for USB re-enumeration) and other
  serial errnos that have unhelpful default text.

### Changed

- `--input cardreader` now auto-detects the best available reader (ORGA
  via USB VID/PID `0780:1202`, otherwise PC/SC) instead of forcing
  PC/SC. Pass `--input pcsc` or `--input generic` to keep the previous
  PC/SC-only behaviour.
- ORGA driver no longer falls back to the first `/dev/cu.usbmodem*` node
  when the USB probe finds no matching terminal — stale device nodes can
  outlive their USB devices for several seconds on macOS, and opening
  one previously produced a confusing ENXIO downstream. It now refuses
  with a clear "no ORGA terminal detected (VID/PID …) — check DFU mode"
  error.

### Tests

- New ORGA mock-serial infrastructure (`internal/reader/orga/mock_test.go`)
  — scriptable `fakeSerialIO` plus `chunkedReader` / `errAfterReader` /
  `errCloser` helpers — enables hardware-free testing of the T=1
  transport, CT-BCS commands, and exchange state machine.
- `internal/reader/orga`: 9.8 % → 83.6 % coverage.
- `internal/reader`: 15.0 % → 62.1 % (factory routing, probe helpers,
  DeviceInfo formatting, session delegators).
- `internal/c2c`: 77.1 % → 83.8 % (handshake error branches, getters,
  Phase.String fallback).
- `internal/egk`: 80.0 % → 83.9 % (`certFields` + DF.ESIGN diagnostic
  rows).
- Overall statement coverage: 66.3 % → 73.6 %.
- Remaining gaps are hardware-bound entry points (`orga.Open`,
  `openSerial`, `generic.Open`) and the debug-CLI `main()` ceremonies in
  `cmd/orga-probe` / `cmd/card-probe`.

### Docs

- `docs/orga-driver/` — seven-document investigation of the CT-BCS over
  CDC-ACM driver development (hardware survey, CT-API spec digest,
  existing-implementation tour, plan, probe log, framing hypotheses,
  card recovery).
- `docs/c2c/` — gemSpec_COS chapter 13 plan, four-walls model of what
  blocks live execution, PIN-workflow notes, CVC-root research, slot-2
  SMC-B analysis.
- `docs/reader-architecture.md` — layered reader / USB / driver diagram
  showing how the orga and PC/SC paths share the `Session` interface.
- README — ORGA support, per-platform setup, auto-detect behaviour,
  reader-driver layering, C2C handshake status.

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
