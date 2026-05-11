# Finding — slot-2 test SMC-B carries no CV-certs

Date: 2026-05-11

## Summary

The slot-2 Praxis-Beutlín T-Systems TCOS test SMC-B (identified earlier in
[../orga-driver/cards/slot2-smcb-test.md](../orga-driver/cards/slot2-smcb-test.md))
does **not** carry CV-certificates at any of the gemSpec_PKI / gemSpec_SMC-B
candidate file identifiers. It is a stripped-down test card for X.509
electronic-signature testing only.

This narrows wall #2 of the four-walls model
([../../memory location](../../../..<redacted>))
from "expired test SMC-B" to "expired test SMC-B that also has no CV-cert
content to read."

## Evidence

`internal/c2c/discover.go` was run against slot 2 (after recovering the
card from a "Fatal Error 3" degraded state via CT-BCS REQUEST ICC P2=01).
Every candidate FID returned `6A82` ("file not found") for the CV-cert
EFs documented in gemSpec_SMC-B:

| AID                       | FID  | Label                                  | SW   |
|---------------------------|------|----------------------------------------|------|
| DF.SMA (D27600000144 8000) | C509 | EF.C.HCI.AUTD_RPS_CVC.E256             | 6A82 (AID itself absent) |
| DF.SMA                    | C50A | EF.C.HCI.AUTR_CVC.E256                 | 6A82 |
| DF.SMA                    | C002 | EF.C.CA_HCI_OSIG.CS.E256               | 6A82 |
| DF.ESIGN (A0…ESIGN)       | C509 | EF.C.HCI.AUTD_RPS_CVC.E256 (fallback)  | 6A82 |
| DF.ESIGN                  | C50A | EF.C.HCI.AUTR_CVC.E256 (fallback)      | 6A82 |
| MF                        | C5*  | broad sweep (C500/08/09/0A/0B/0C/0F)   | 6A82 |
| MF                        | C0*  | broad sweep (C002/09)                  | 6A82 |
| MF                        | 2F1* | broad sweep (2F11/12/13)               | only 2F11 = EF.Version (40 bytes, TCOS version block — not a cert) |
| MF                        | D0*  | broad sweep (D080/09)                  | 6A82 |
| DF.ESIGN                  | C5*  | C507/08/09/0A/0B sweep                 | 6A82 |
| DF.ESIGN                  | C0*  | C002/09/0F sweep                       | 6A82 |

DF.ESIGN's known content (from `../orga-driver/cards/slot2-certs/`):
SFI 1 = `C500` (X.509 RSA auth), SFI 2 = `C200` (X.509 RSA enc), SFI 5/6 = unknown
X.509 (DER `30 82 03 …`), SFI 7 = `C506` (Brainpool P256r1 ECDSA auth).
All five are X.509 leaf certs (DER SEQUENCE `30 82 …`) — none is a CV-cert
(BER tag `7F21`).

## Implications

- **`internal/c2c/discover.go`** is correct (unit-tested with synthetic
  CV-certs in `internal/c2c/discover_test.go`) but has no live data to
  exercise against this hardware.
- The slot-2 card cannot drive a full live C2C handshake against the
  real eGK in slot 1 regardless of what we do — there is nothing to
  send to the eGK during `PhasePresentToVerifier`.
- This is independent of (and additional to) the existing problems with
  this card: TEST-ONLY CA issuance and expiry 2024-12-11.

## Recovering the card after Fatal Error 3

If `SELECT MF` starts returning `64A2` on slot 2 (TCOS-specific
"card not in operational state" — likely after a terminal reset), force a
slot power-cycle by issuing the CT-BCS REQUEST ICC with P2=01 against the
terminal:

```
./orga-probe -slot 0 -apdu "20 12 02 01 00"
```

This returns the ATR (with SW 6201) and restores the card to operational
state. The terminal's own EJECT (INS=0x15) returned `SW=6700 Wrong length`
on this firmware — REQUEST ICC P2=01 is the working recovery path.

## Path forward

To actually exercise the live C2C handshake we need either:

1. A production SMC-B with current gematik-CA-issued CV-certs (out of
   reach — requires being a registered Leistungserbringer).
2. A test SMC-B that does include CV-certs (some gematik test-card
   distributions carry them; this particular T-Systems TCOS one does not).

Until then the implementation can progress on synthetic CV-cert fixtures
in unit tests, plus the parallel research stream looking for the gematik
CVC-Root public keys themselves (see `cvc-root-research.md` when the
research agent lands its output).
