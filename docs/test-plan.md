# Unit-test plan

Baseline coverage (initial run, only the existing round-trip tests in `internal/document`): **28.4 %** overall.
Target: **≥ 90 %** for everything except the irreducibly external boundaries (PC/SC, the live GKV-Datenaustausch HTTP index, `os.Exit`).

**Achieved**: **87.1 %** overall, **96–100 %** on every `internal/*` package. The shortfall on the global number is the deliberate PC/SC integration glue in the `cmd/*` binaries; see the per-package table below.

## Achieved per package

| Package | Coverage | Notes |
| --- | --- | --- |
| `internal/output` | **100.0 %** | |
| `internal/document` | **96.5 %** | gaps are: error branches in `encodeGDT6301` and `ParseGDT` that are exercised by the round-trip but not the individual error helpers — accept as-is |
| `internal/egk` | **95.9 %** | gaps in `Read`/`readEFCombined` are the FID-fallback after the SFI-empty path; exercised at the function level but not byte-for-byte |
| `internal/ktda` | **95.7 %** | gaps in `Parse` are early `nil`-return guards; `downloadOne` `io.Copy` failure path skipped (hard to trigger reliably) |
| `cmd/card-reader` | **68.8 %** | uncovered: `setupCardReader`, `waitForCard`, `runDebug`, the cardreader branch of `loadCardData`/`Run`, `main()`. All PC/SC integration — see **Accepting deliberate gaps** below |
| `cmd/card-probe` | **22.6 %** | uncovered: `main`, `probeReader`, `transmit`, `die`. Pure PC/SC probe utility; only the three pure helpers (`h`, `parseICCSN`, `guessFromATR`) are unit-tested |

## Approach

1. Add tests for every package without source changes first — get a coverage baseline.
2. For each function that is unreachable from a unit test today, list the *exact* reason and the smallest possible source change that unblocks it. Discuss with the user before touching the source.
3. After agreed changes land, add the remaining tests.

No production code edits anywhere in this plan until each blocker has been signed off individually.

## Coverage table (per package)

| Package | Files | Testable now | Blocked (and why) |
| --- | --- | --- | --- |
| `internal/egk` | `parse.go`, `form.go` | `FormatDate`, `FormatGender`, `FormatInsuredType`, `FormatCountry`, `xmlCharsetReader`, `unmarshalXML`, `gunzip`, `ParsePD`, `ParseVD`, `FormField.Filled`, `FormMapping`, `explainInsuredType`, `explainWOP`, `vknrValue/Source/Note`, `ktgValue/Source/Note`, `ktabFromIKInfo`, `ktabSource`, `ktabNote`, `copayNote` | `apdu.go` and `egk.go::Read`/`readEFCombined` — see **Blocker A** |
| `internal/ktda` | `store.go`, `ke0.go`, `fetch.go` | All of `store.go`, all of `ke0.go`, `KassenartFromFilename`, `Download`, `downloadOne` (via `httptest.NewServer`) | `DiscoverFiles` — see **Blocker B** |
| `internal/document` | already has 4 test files | All encoders, all parsers, all three table renderers, `captureBytes`, `billingIK`, `billingName`, helpers (`hl7Escape`, `hl7Unescape`, `gdtDate`, `gdtSex`, etc.) | None |
| `internal/output` | `output.go` | `File.Write`, `Multi.Write`, `Stdout.Write` (via `os.Pipe` redirect) | None |
| `cmd/card-reader` | `cli.go`, `render.go`, `glossary.go`, `ktda_cmd.go`, `cardreader.go` | `outputForPath`, `encoderKey`, `ReadCmd.Validate`, `wrap`, `chrome`, `renderForm`, `renderTable`, `renderGlossary`, `glossaryTable`, `suggestBaseName`, `decodeState`, `defaultKTDAPath`, `resolveIK`, `warnIfStale`, `orDash`, `loadCardData` (file branch only), `KtdaLookupCmd.Run`, `KtdaInfoCmd.Run` (against a temp ktda.json) | `setupCardReader`, `waitForCard`, `runDebug`, `ReadCmd.Run` (cardreader path), `KtdaUpdateCmd.Run`, `runKTDAUpdate`, `ensureKTDA` auto-fetch — see **Blocker A**, **Blocker B** |
| `cmd/card-probe` | `main.go` | `h`, `parseICCSN`, `guessFromATR` | `main`, `probeReader`, `transmit`, `die` — see **Blocker A**, **Blocker C** |

## What gets tested where (testable-now items)

### `internal/egk/parse.go`
- `FormatDate`: `"19720314" → "1972-03-14"`, `"" → ""`, `"abc" → "abc"` (non-numeric short-circuit), 7-digit → unchanged.
- `FormatGender`: M/W/F/X/D/empty/unknown.
- `FormatInsuredType`: 1/3/5/empty/other.
- `FormatCountry`: D, A, CH, F, NL, PL, empty, unknown.
- `xmlCharsetReader`: iso-8859-15, iso-8859-1, utf-8, empty (→ pass-through), unsupported → error.
- `unmarshalXML`: round-trip on a small struct via ISO-8859-15 declared encoding.
- `gunzip`: known gzip blob → bytes; truncated → error.
- `ParsePD`: encode a `PersonalData` struct, gzip it, prepend the 2-byte length, parse back. Error path: short buffer, length-exceeds-buffer, bad gzip, malformed XML.
- `ParseVD`: encode an `InsuranceData` + `ProtectedData`, build the 8-byte offset header + two gzipped sections, parse back. Error paths: short header, no GVD section (gvdStart=0), bad gzip in AVD, malformed AVD XML, bad gzip in GVD (silently dropped).

### `internal/egk/form.go`
- `FormField.Filled`: with empty / whitespace / real value.
- `FormMapping`: with full `CardData` + `IKInfo`, with `nil` IKInfo, with empty `Personal`/`Insurance`/`Protected`, with `BesondereGruppe`/`DMP` blank (defaults to "00"), with `AbrechnenderKostentraeger` missing (falls back to issuing IK), with `ZuzahlungStatus="1"` (copay date set) and "0" / "".
- Each helper (`explainInsuredType`, `explainWOP`, `vknrValue/Source/Note`, `ktgValue/Source/Note`, `ktabFromIKInfo/Source/Note`, `copayNote`) tested with the full code matrix.

### `internal/ktda/store.go`
- `Compile`: merge two slices, dedupe-by-IK with `bestOf` rules — verify VKNR-preferred wins, then later-ValidFrom wins.
- `bestOf` (called via `Compile` table-driven cases).
- `Save` + `Load` + `ReadStore`: round-trip a fixture `Store` via `t.TempDir()`.
- `Lookup`: present, absent, nil receiver.
- `Kostentraegergruppe`: all 6 prefixes + unknown + empty.
- `Stats`: nil store, empty store, store with mixed VKNRs.
- `SortedIKs`: stable order, nil receiver.

### `internal/ktda/ke0.go`
- `Parse`: hand-crafted minimal KE0 byte stream with UNH+IDK+VDT+NAM+VKG+UNT → one `Entry` with all fields populated. Plus: multiple messages, message without UNT (final flush), UNT before UNH (no-op), embedded CR/LF inside segments, `?+` release escape.
- `splitEDIFACT`: covered indirectly + a direct table-driven test for the release-character behaviour.
- `handleSegment`: covered via `Parse`; one direct test for `IDK`-before-UNH (creates entry).
- `ParseError.Error`: string format.

### `internal/ktda/fetch.go`
- `KassenartFromFilename`: AO/EK/BK/IK/BN/LK + lowercase + short string + full path.
- `Download`: spin up `httptest.NewServer`, return small bodies; assert atomic `.tmp` rename, file content matches, returned paths are correct, 404 returns an error.
- `downloadOne`: same server; test crash-recovery by injecting a `io.Writer` failure (need source change — see **Blocker B**; deferred to optional).

### `internal/document` (gaps in existing tests)
- `gdt.go` direct helpers: `gdtLine` (ISO-8859-15 length math against German umlauts), `gdtDate` (valid / invalid / short / non-numeric), `gdtSex`, `gdtVKNR` (nil / set).
- `hl7v2.go` direct helpers: `hl7Escape` (each of the five delimiters individually + combinations + empty), `hl7Name` (truncation of trailing empty parts), `hl7Address` (with/without `AddressSuffix` and `HouseNumber`), `hl7Sex`, `hl7VKNR`, `condDate`, `segment`.
- `hl7v2_parse.go` direct helpers: `fieldAt`, `compAt`, `firstRep`, `splitComps`, `hl7Unescape` (each escape sequence, unknown `\Xxx\` pass-through, malformed missing-trailing-escape, non-default delimiters), `parseHL7Sex`, `condDateValue`.
- `fhir.go` direct helpers: `fhirPatientID` (empty → `patient-unknown`), `fhirCoverageID` (all three branches), `fhirGender`, `fhirAddress` (empty street/city/postcode → `nil`; with/without suffix), `fhirCodingExtension`.
- `fhir_parse.go` direct helpers: `parseFHIRGender`, `stripDashes`, `extensionCode` (valueCoding vs valueString fallback), `patientToPersonal` with `_family` extensions (namenszusatz, own-prefix), `coverageToInsurance` with each extension URL (versichertenart, wop, besondere-personengruppe, dmp-kennzeichen, unknown).
- `document.go`: `captureBytes` failure path (function returns error → no Document); `billingIK`/`billingName` with the three branches each.
- `gdt_table.go::RenderGDT`: smoke + check 8100 placeholder gets patched; with `nil` data; with `ZuzahlungStatus="1"` adds 4242.
- `fhir_table.go::RenderFHIR`: smoke + check empty fields render as em-dash; `BesondereGruppe="00"`/`DMP="00"` suppressed; helper `systemOrDash`, `patientRef`, `addrUse`, `addrType`, `fhirWrap`.
- `hl7v2_table.go::RenderHL7ADT`: smoke + with `nil` data.
- `json.go::formMappingJSON.Encode`: produces valid JSON array of 21 rows matching `FormMapping`.

### `internal/output`
- `File.Write`: writes the bytes under `t.TempDir()`, basename + extension are honoured, mkdir is implicit, contents match.
- `Stdout.Write`: redirect `os.Stdout` to a pipe, write a doc, verify bytes.
- `Multi.Write`: a real `File` writer + an in-memory `Writer` (defined in the test file) — both get the same bytes; a failing inner writer aborts the chain.

### `cmd/card-reader`
- `cli.go::outputForPath`: `.gdt`/`.hl7`/`.fhir.json`/`.json` + unknown → error + case-insensitive.
- `cli.go::encoderKey`: hl7-fhir → fhir, hl7-adt → hl7adt, gdt → gdt, json → json, form → form.
- `cli.go::ReadCmd.Validate`: default output when input==cardreader, default from path, illegal `--output` value, `--file --output form` rejected, `--debug --input file.gdt` rejected.
- `render.go::wrap`: long string with multi-word wrapping.
- `render.go::renderForm` / `chrome` / `renderTable`: smoke tests for each format and the `glossary=true` branch.
- `glossary.go::renderGlossary`, `glossaryTable`: smoke; assert section captions are present.
- `cardreader.go::suggestBaseName`: with/without KVNR, with/without `Personal`.
- `cardreader.go::decodeState`: each scard flag bit + combinations + zero (`NONE`).
- `cardreader.go::loadCardData` (file branch only): `.gdt` / `.hl7` / `.fhir.json` paths via fixtures written under `t.TempDir()`; nonexistent path → error; unknown extension → error.
- `ktda_cmd.go::defaultKTDAPath`: with no file present → cwd fallback; with file present in cwd → returns it.
- `ktda_cmd.go::resolveIK`: nil data, missing Insurance, missing IK, store-not-loaded (using `t.Chdir` + no ktda.json on disk + DiscoverFiles will fail → expect nil), store-loaded hit and miss.
- `ktda_cmd.go::warnIfStale`: stale Q (older year), stale Q (same year earlier quarter), current quarter (no warn), nil store, source filenames that don't match the regex.
- `ktda_cmd.go::orDash`: empty → "-", value → value.
- `ktda_cmd.go::KtdaLookupCmd.Run`: with a fixture ktda.json in cwd → exits 0, prints expected lines (capture stdout via `os.Pipe`); IK missing → error; ktda.json missing → error.
- `ktda_cmd.go::KtdaInfoCmd.Run`: same as Lookup with a fixture file.

### `cmd/card-probe`
- `h`: hex decode happy + bad hex (test must catch the panic via `recover`).
- `parseICCSN`: TLV form (5A 0A …), raw 10-byte, malformed (too short, wrong tag).
- `guessFromATR`: each pattern branch + unknown.

## Blockers — small source changes that were applied

### Blocker A — `*scard.Card` was a concrete type, not an interface [APPLIED]

**Was affected:** `internal/egk/apdu.go` (`transmit`, `selectMF`, `selectByAID`, `selectEF`, `readBinary`, `readEFBySFI`), `internal/egk/egk.go` (`Read`, `readEFCombined`).

**Fix:** added [internal/egk/card.go](../internal/egk/card.go) — a 5-line `Card` interface with `Transmit([]byte) ([]byte, error)`. Swapped every `*scard.Card` parameter to `Card`. `*scard.Card` satisfies the interface structurally; zero call-site changes outside `internal/egk`.

**Result:** `internal/egk` went from 0 % to 95.9 %.

**Not changed:** no equivalent interface for `*scard.Context` — that would only help `cmd/card-reader/cardreader.go::setupCardReader`/`waitForCard`/`runDebug`, which are PC/SC integration territory and stay uncovered by design. Similarly nothing in `cmd/card-probe` was touched.

### Blocker B — `ktda.DiscoverFiles` hardcoded the index URL [APPLIED]

**Was affected:** `internal/ktda/fetch.go::DiscoverFiles`.

**Fix:** `IndexURL` and `baseHost` in [internal/ktda/fetch.go](../internal/ktda/fetch.go) changed from `const` to `var`. Tests in [internal/ktda/fetch_test.go](../internal/ktda/fetch_test.go) swap them to `httptest.NewServer().URL` for the duration of each test.

**Result:** `internal/ktda` went from 0 % to 95.7 %.

### Blocker C — `cmd/card-probe/main.go::die` calls `os.Exit` [SKIPPED, AS RECOMMENDED]

Three trivial lines, not worth a refactor.

## Accepting deliberate gaps

These are not blockers but lines that should *not* be hit by unit tests by design:

- `main()` in both binaries — wires up kong/PC/SC, integration-test territory. ~30 lines.
- `cmd/card-reader/cardreader.go::setupCardReader`, `waitForCard`, `runDebug` and the cardreader branch of `loadCardData`/`Run` — all sit on top of `*scard.Context`/`*scard.Card`. The card branch of `loadCardData` is ~25 lines, `runDebug` is ~50 lines, `waitForCard` is ~20 lines, `setupCardReader` is ~15 lines. ~110 lines.
- `cmd/card-probe/main.go::main`, `probeReader`, `transmit`, `die` — pure PC/SC probe utility. The three pure helpers (`h`, `parseICCSN`, `guessFromATR`) ARE unit-tested. ~100 lines uncovered.
- `gunzip` error branch (`io.ReadAll` failure on a closed reader) — Go runtime, not our code.
- `Save` permission errors — covered when the parent dir is missing, more isn't useful.

That's roughly 240 lines of PC/SC integration glue that's intentionally out of scope. The remaining ~3500 lines of project logic are at **96–100 %** coverage.

## Running the tests

```sh
go test ./...                                    # all tests
go test -coverprofile=cov.out ./...              # with coverage
go tool cover -html=cov.out                      # interactive coverage report
go tool cover -func=cov.out | tail -1            # overall percentage
```
