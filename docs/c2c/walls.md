# The four walls

PIN-protected eGK reads require **all four** of the following to be true.
Solving any one in isolation buys nothing — the chain only completes if
all four hold simultaneously. This is the canonical project model used by
discussions in this repo, the parent README, and the C2C code itself.

## Wall 1 — Driver: the host can talk to the terminal

✅ **Solved.** The Ingenico/Worldline ORGA 9xx terminal works on macOS
without a vendor driver:

- Enumerates as USB-CDC-ACM via the in-kernel `AppleUSBCDCACM` driver.
- Wire framing is standard **ISO 7816-3 T=1** with LRC EDC over the
  CDC-ACM serial endpoint at 9600 8N1. No proprietary wrapper.
- USB identity is matched by VID `0x0780` / PID `0x1202` (the "ORGA 900
  Smart Card Terminal Virtual Com Port" family, which covers the
  930 M) — see [`internal/reader/usb/`](../../internal/reader/usb/) and
  [../orga-driver/01-hardware.md](../orga-driver/01-hardware.md).
- Go transport: ~250 LoC across `internal/reader/orga/{orga,exchange,
  transport,ctbcs,safety,serial_darwin}.go` plus a cross-OS USB probe
  in `internal/reader/usb/{darwin,linux,windows,other}.go`.

What was previously documented (Worldline/Ingenico's
"Windows-only driver required" claim) turned out to be misleading. The
binary `ctorg32.dll` / `libctorgt1.so` they ship implements the same
ISO 7816-3 T=1 framing we now do natively.

Reference: full investigation log in [../orga-driver/](../orga-driver/).

## Wall 2 — SMC-B with valid CV-certs

❌ **Blocked.** The user's slot-2 card cannot be made to satisfy this wall.

A C2C handshake against an eGK requires the partner card (typically an
SMC-B = Security Module Card type B for healthcare institutions) to
present a CV-certificate chain that:

1. **Exists on the card.** The SMC-B must carry CV-certs at a known FID
   (typically under DF.SMA at `D27600000144 8000`, FIDs `C509`/`C50A`
   for AUT/AUTR, plus a CA cert under DF.SMA or MF).
2. **Is currently valid.** Each cert's `NotBefore` ≤ now ≤ `NotAfter`.
3. **Chains to gematik's production CVC-Root** (CHR prefix `DEZGW…`,
   currently `DEZGW870226`). The eGK trusts only the production CVC-Root,
   not the test one (`DEGXX…`). See [cvc-root-research.md](cvc-root-research.md).

The user has access to one SMC-B-shaped card: the Praxis-Beutlín
T-Systems TCOS card in slot 2 of their ORGA terminal. It fails all three
tests:

- **No CV-certs on the card.** A complete FID sweep
  ([slot2-no-cvcerts.md](slot2-no-cvcerts.md)) found only X.509 certs
  under DF.ESIGN. No CV-cert (tag `7F21`) was found anywhere. DF.SMA
  itself returns `6A82` (file not found) — the application is absent.
- **X.509 certs expired** 2024-12-11.
- **Issued by gematik TEST-ONLY CA** (`GEM.SMCB-CA24 TEST-ONLY`), which
  chains to the test CVC-Root, not production.

Acquiring a production SMC-B requires registration as a Leistungserbringer
in the German healthcare system — a Krankenversicherungs-Approbation
(physician's license) or equivalent for psychotherapists, pharmacists,
etc. The user does not currently have one available.

## Wall 3 — Cardholder PIN

🟡 **In hand, gated.** The user has — or can obtain — the PIN, but using
it is blocked behind a mechanical safety guard until Wall 2 is also solved.

PIN provenance, structure, and counter math: see
[pin-workflow.md](pin-workflow.md).

In code: any APDU with `INS=0x20` (VERIFY) on any non-CT-BCS CLA is
refused by [`internal/reader/orga/safety.go`](../../internal/reader/orga/safety.go)
unless `Options.AllowPINWrite` is set (or `-UNSAFE-allow-pin-write` is
passed to `orga-probe`). This is because every wrong VERIFY decrements
the on-card retry counter, three strikes locks the PIN, and the counter
never decays over time — see gemSpec_COS chapter 9.

We will only ever issue a VERIFY when phases 1-5 of C2C have completed
and the eGK has accepted the SMC-B authentication, because that's the
sequence that maximises the chance of a single VERIFY succeeding.

## Wall 4 — gemSpec_COS C2C implementation in code

✅ **Solved.** All five phases of the handshake are implemented and
unit-tested:

| Phase | File | Tests |
|---|---|---|
| 1. Discover SMC-B CV-certs | [`discover.go`](../../internal/c2c/discover.go) | 3 |
| 2. Validate chain locally | [`handshake.go`](../../internal/c2c/handshake.go) + `keys.VerifyChain` | 6 |
| 3. PSO VERIFY CERTIFICATE chain to eGK | [`phase_present.go`](../../internal/c2c/phase_present.go) | 8 |
| 4. Mutual auth (MSE SET AT / GET CHALLENGE / INT-AUTH / EXT-AUTH) | [`phase_mutual.go`](../../internal/c2c/phase_mutual.go) | 7 |
| 5. AES-128 key derivation → `*sm.Session` | [`phase_secure.go`](../../internal/c2c/phase_secure.go) | 7 |

Plus four supporting subpackages:

- [`internal/c2c/cvcert/`](../../internal/c2c/cvcert/) — CV-cert
  ASN.1 BER-TLV parser (21 tests)
- [`internal/c2c/brainpool/`](../../internal/c2c/brainpool/) — Brainpool
  P256/384/512r1 + ECDSA verify (19 tests)
- [`internal/c2c/keys/`](../../internal/c2c/keys/) — gematik X.509 TSL
  roots + 4 CVC-Roots (PU.7 active + RU-TU.6/7/8) + chain validation
  (17 tests)
- [`internal/c2c/sm/`](../../internal/c2c/sm/) — gemSpec_COS Secure
  Messaging Wrap/Unwrap + AES-CMAC (31 tests)

Total: ~~400 tests passing across all c2c packages.~~ 404 across the
whole repo at the time of writing.

The code is production-shaped — if a CV-cert-bearing SMC-B materialises,
`cmd/orga-probe -c2c 2 --c2c-test-roots=false` would attempt the live
handshake against the eGK. Until then, the implementation is exercised
only with synthetic test fixtures.

## Why Wall 2 dominates

If only Walls 1, 3, and 4 are satisfied (which is the current state),
the host is still completely unable to read PIN-protected eGK data —
because step 3 of the handshake (PSO VERIFY CERTIFICATE on the eGK) will
return `SW=6300` ("authentication failed") as soon as it sees a cert
that doesn't chain to the eGK's on-card production CVC-Root.

The four-walls model exists precisely because this is non-obvious — most
"can I read my health card?" reasoning assumes Wall 3 alone is enough
("I have the PIN, why doesn't it work?"). The answer is that the PIN is
the *third* check on a sequence that won't even start unless Wall 2 is
satisfied first.
