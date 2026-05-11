# gematik PKI root keys — provenance log

This file records exactly where each root public key embedded in
`roots.go` came from, when it was fetched, and how to reproduce the
embedded modulus and SHA-256 fingerprint. Treat it as source-of-truth
documentation for trust decisions.

## Fetch environment

- Date: 2026-05-11
- Tools: `curl 8.7+`, `openssl 3.x`
- Host: macOS (darwin), but only TLS+HTTP fetches; nothing platform-specific.

## Production roots (https://download.tsl.ti-dienste.de/ROOT-CA/)

All three are self-signed RSA-2048-SHA-256 (`sha256WithRSAEncryption`),
exponent 65537.

### GEM.RCA2

| Field         | Value |
| ------------- | ----- |
| URL           | https://download.tsl.ti-dienste.de/ROOT-CA/GEM.RCA2.der |
| HTTP status   | 200 OK, 1016 bytes |
| SHA-256       | `848fda162c607b492c62f625840e6451285c40c7334ec8dd659d093236ebc9ec` |
| Subject DN    | `C=DE, O=gematik GmbH, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA2` |
| Self-issued?  | yes (issuer == subject) |
| Validity      | 2016-12-09 08:41:56 UTC → 2026-12-07 08:41:56 UTC |
| Serial        | `01` |
| SubjectKeyId  | `ec5c18e013b4436c098cdffa3c3c5b7e4b708446` |

### GEM.RCA6

| Field         | Value |
| ------------- | ----- |
| URL           | https://download.tsl.ti-dienste.de/ROOT-CA/GEM.RCA6.der |
| HTTP status   | 200 OK, 1065 bytes |
| SHA-256       | `7c250199c7d87058a3a8f84f2a3c7727a27511670dac596535273af0452d84f3` |
| Subject DN    | `C=DE, O=gematik GmbH, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA6` |
| Self-issued?  | yes |
| Validity      | 2021-11-11 08:50:44 UTC → 2031-11-09 08:50:44 UTC |
| Serial        | `01` |
| SubjectKeyId  | `1844c4da66933aef4f3b2a0965e4fe28901e5511` |

### GEM.RCA9

| Field         | Value |
| ------------- | ----- |
| URL           | https://download.tsl.ti-dienste.de/ROOT-CA/GEM.RCA9.der |
| HTTP status   | 200 OK, 1065 bytes |
| SHA-256       | `b7eee57557c31d43263d5e6cfe98185acf2b7d338c2261a054368d5dd5432442` |
| Subject DN    | `C=DE, O=gematik GmbH, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA9` |
| Self-issued?  | yes |
| Validity      | 2025-06-04 08:27:44 UTC → 2035-06-02 08:27:44 UTC |
| Serial        | `01` |
| SubjectKeyId  | `96e4a429a0436ed9103632a141ef01f33e18e936` |

Note: GEM.RCA3 .. GEM.RCA5, GEM.RCA7, GEM.RCA8, GEM.RCA10 returned
404 at the time of fetch. The TSL only publishes the active generations.

## TEST-ONLY roots (https://download-test.tsl.ti-dienste.de/ROOT-CA/)

Same parameters as production but with subject suffix " TEST-ONLY" and
organisation " gematik GmbH NOT-VALID". These anchor the gematik test
PKI; real eGK / SMC-B / HBA cards reject any chain rooted here.

### GEM.RCA2 TEST-ONLY

| Field         | Value |
| ------------- | ----- |
| URL           | https://download-test.tsl.ti-dienste.de/ROOT-CA/GEM.RCA2_TEST-ONLY.der |
| HTTP status   | 200 OK, 1066 bytes |
| SHA-256       | `074609b1d76a19286efcb90634a0d6aea36826ee1ffc52c696235b7f4a87872d` |
| Subject DN    | `C=DE, O=gematik GmbH NOT-VALID, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA2 TEST-ONLY` |
| Validity      | 2016-11-17 15:50:57 UTC → 2026-11-15 15:50:57 UTC |
| SubjectKeyId  | `2d6900bba1f4cc8e03a2258392c9d263e1d944b8` |

This is the root that anchors the slot-2 SMC-B in this project:
- leaf: `docs/orga-driver/cards/slot2-certs/sfi1-c500.pem`
  - issuer: `CN=GEM.SMCB-CA24 TEST-ONLY`
  - AuthorityKeyId: `7AE9E16FEA14591605EE03E9D3FD21ABDEE9D99E`
- That AKI is the SubjectKeyId of the `GEM.SMCB-CA24 TEST-ONLY`
  intermediate, which is itself issued by `GEM.RCA2 TEST-ONLY`. The
  intermediate is listed in the TEST-PKI TSL.xml; if/when integration
  needs the intermediate, fetch it from the same TEST TSL endpoint.

### GEM.RCA6 TEST-ONLY

| Field         | Value |
| ------------- | ----- |
| URL           | https://download-test.tsl.ti-dienste.de/ROOT-CA/GEM.RCA6_TEST-ONLY.der |
| HTTP status   | 200 OK, 1115 bytes |
| SHA-256       | `3cff0528cf0ff06e5a99f157afad505bca9dfa012861a471f71ca98cea5721ed` |
| Subject DN    | `C=DE, O=gematik GmbH NOT-VALID, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA6 TEST-ONLY` |
| Validity      | 2021-10-28 07:24:14 UTC → 2031-10-26 07:24:14 UTC |
| SubjectKeyId  | `4cf7e065585598e6398bc807753d4ca6702ccf29` |

### GEM.RCA9 TEST-ONLY

| Field         | Value |
| ------------- | ----- |
| URL           | https://download-test.tsl.ti-dienste.de/ROOT-CA/GEM.RCA9_TEST-ONLY.der |
| HTTP status   | 200 OK, 1115 bytes |
| SHA-256       | `75bb81da87f1841dc667868149df04e469c2006e1da0d27eb1c46d0a234c55bd` |
| Subject DN    | `C=DE, O=gematik GmbH NOT-VALID, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA9 TEST-ONLY` |
| Validity      | 2025-05-08 12:01:40 UTC → 2035-05-06 12:01:40 UTC |
| SubjectKeyId  | `a6a40647805e06b66d2d6dc9dbc367d284477cd0` |

The mirror at `https://download-ref.tsl.ti-dienste.de/ROOT-CA/` serves
byte-identical content; either host works.

## Reproduction recipe

To re-derive any fingerprint:

```sh
curl -fsSo /tmp/r.der "https://download.tsl.ti-dienste.de/ROOT-CA/GEM.RCA6.der"
shasum -a 256 /tmp/r.der
openssl x509 -inform der -in /tmp/r.der -noout -subject -issuer -startdate -enddate -modulus
openssl x509 -inform der -in /tmp/r.der -noout -ext subjectKeyIdentifier
```

The modulus printed by `openssl -modulus` (with the `Modulus=` prefix
stripped and converted to lowercase) is exactly the constant embedded
in `roots.go` (`gemRCA<N>ModulusHex`).

## What is NOT embedded — TODO

### CVC-Root keys (gemSpec_COS §13)

The card-to-card handshake terminates at gematik's CVC-Root, a parallel
PKI distinct from the X.509 TSL hierarchy above. CVC-Root keys are
typically Brainpool P-256 or P-384. They are NOT served from the TSL
endpoint and must be obtained from:

- gemSpec_PKI annexes (the public-key bytes are published in the
  spec PDF tables; the spec itself is the trust anchor)
- gemSpec_CVC_Root if a separate document with that name applies for
  the current generation
- Fachportal: https://fachportal.gematik.de/

Until the production CVC-Root key is located and reviewed, true C2C
authentication against a real eGK cannot be exercised. The current
deliverable validates the X.509 hierarchy used for the certificates of
approval embedded on the card (slot-2 SMC-B `sfi1-c500`), which still
exercises the full VerifyChain state machine including time validity,
name binding and signature verification.

### Intermediate CA certificates

Trust-anchor verification only needs the roots above. To fully verify
a leaf X.509 cert from the card, callers must additionally collect the
intermediate (e.g. `GEM.SMCB-CA24 TEST-ONLY`) from the same TSL or
embedded in the card under SFI 7 (`C.CA.CS`). That is integration-layer
work, not part of this subpackage.

## Cross-check policy

If any embedded modulus or fingerprint ever fails to match the upstream
DER on re-fetch, treat that as a trust failure: pause integration,
investigate whether gematik re-issued the root, and update both
`roots.go` and this file in the same commit. Do NOT silently rotate
constants.
