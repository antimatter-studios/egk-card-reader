# Cards discovered

This directory holds structured identity reports for every smart card we've
probed via the ORGA. One markdown file per (card, slot) pair, plus any
extracted DER/PEM certificates that came along.

Generate with `./orga-probe -identify <slot> -identify-out docs/orga-driver/cards/<name>.md`.

## Catalogue

| File                 | Slot | Card class                       | Live for production? |
|----------------------|------|----------------------------------|----------------------|
| [slot1-egk.md](slot1-egk.md) | 1 (front) | eGK G1 of the project author | yes (real card, ICCSN redacted) |
| [slot2-smcb-test.md](slot2-smcb-test.md) | 2 (back) | T-Systems TCOS 2.0 SMC-B, GEM.SMCB-CA24 TEST-ONLY, **expired 2024-12-11** | **no** |

## Slot-2 SMC-B summary

- **Subject**: `Praxis Torben-Tom Graf Beutlín TEST-ONLY` — placeholder German healthcare practice (Flensburger Str. 61, 24837 Schleswig)
- **Issuer**: `GEM.SMCB-CA24 TEST-ONLY, OU=Institution des Gesundheitswesens-CA, O=gematik GmbH NOT-VALID, C=DE`
- **Validity**: 2020-01-27 → **2024-12-11** (expired)
- **Algorithms**: RSA-2048 (C500 auth + C200 enc) AND **ECDSA Brainpool P256r1** (C506 auth — Go stdlib does NOT decode this)
- **Chip**: Infineon SLE78144
- **OS**: T-Systems TCOS 2.0 (Service Box SB20 v2.3.0)
- **OCSP**: `http://ehca.gematik.de/ocsp/` (gematik test OCSP responder, not production)
- **Card-side trust anchor**: gematik **TEST-ONLY** root, not production

## C2C feasibility verdict

This SMC-B is **structurally suitable** for developing/validating a C2C implementation:
- It carries the right object system layout (DF.ESIGN with expected FIDs).
- It contains both RSA and ECDSA private keys (the auth keys behind C500/C506).
- It will participate in a C2C handshake initiated by any compatible card.

It is **operationally unsuitable** for unlocking the cardholder's real eGK:
- The eGK validates an SMC-B's CV-cert chain against the **production** gematik root only. CV-certs (separate from these X.509 certs) on this card chain to the TEST root — eGK rejects.
- Even pre-expiry the test cert wouldn't be accepted by a production eGK.

Practical consequence: we can write and unit-test the C2C state machine against this card. End-to-end "decrypt EF.NFD" requires acquiring a production SMC-B with current gematik-CA-signed CV-certs, which requires registration as a Leistungserbringer.

## Certs extracted

See [slot2-certs/](slot2-certs/) for raw DER + PEM dumps. PEM files openable with `openssl x509 -in <file> -text` for the canonical view.
