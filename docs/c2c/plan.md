# C2C implementation plan — gemSpec_COS chapter 13

Goal: implement card-to-card mutual authentication between an SMC-B and an eGK so the host (this Go binary, talking to both cards via the ORGA dual-slot terminal) can request PIN entry and then read PIN-protected EFs (NFD, DPE, eMP, …) on the eGK.

## State of the world

- Driver: ORGA 930 M works on macOS via `internal/reader/orga` (T=1 over CDC-ACM). Both slots reachable; APDUs to slot 1 (eGK) and slot 2 (SMC-B) go through `*orga.Slot.Transmit`.
- Slot 1 eGK: real production card, G1, RSA-2048 era.
- Slot 2 SMC-B: structurally correct SMC-B, but issued by **gematik TEST-ONLY CA** and expired 2024-12-11. Suitable for development; **will not unlock the real eGK end-to-end**. The C2C code itself is the goal.

## Package layout

```
internal/c2c/
├── doc.go               package overview
├── cvcert/              CV-cert ASN.1 parsing (BSI TR-03110 / gemSpec_PKI)
├── brainpool/           BrainpoolP256r1 ECC + ECDSA verify (Go stdlib has no Brainpool)
├── keys/                gematik root pub keys (production + test) + chain verification
├── sm/                  secure messaging wrap/unwrap (gemSpec_COS §10)
└── handshake.go         C2C state-machine orchestrator (integration phase, after agents)
```

## Why these subpackages, why parallel

- Each subpackage has a clean boundary; agents can develop them in isolation.
- `cvcert` is pure ASN.1 — no crypto, no card I/O.
- `brainpool` is pure math — no I/O, no spec dependencies beyond RFC 5639.
- `keys` does research (locating + verifying gematik root keys) + uses `cvcert` + `brainpool`.
- `sm` is APDU-level crypto wrapping — independent of cert/key code.
- The final `handshake.go` ties them together via the orga slots.

## Interfaces the agents must respect

Each agent defines the concrete types in their subpackage. The handshake layer will compose them via these shapes:

```go
// internal/c2c/cvcert
package cvcert

type Cert struct {
    CAR        string         // Certificate Authority Reference (issuer)
    CHR        string         // Certificate Holder Reference (subject)
    NotBefore  time.Time
    NotAfter   time.Time
    KeyAlg     KeyAlg         // RSA2048 | BrainpoolP256r1 | …
    PublicKey  any            // *rsa.PublicKey or *brainpool.Point
    Signature  []byte         // raw signature bytes (over Body)
    Body       []byte         // bytes covered by Signature
    Raw        []byte         // entire tag-7F21 wrapper as received
}

type KeyAlg int
const (
    AlgRSA2048 KeyAlg = iota + 1
    AlgBrainpoolP256r1
    AlgBrainpoolP384r1
)

func Parse(der []byte) (*Cert, error)
```

```go
// internal/c2c/brainpool
package brainpool

type Curve interface {
    Params() *CurveParams
}
type P256r1 struct{}      // satisfies elliptic.Curve enough for Verify
type Point struct{ X, Y *big.Int }

func VerifyP256r1(pub *Point, hashed []byte, r, s *big.Int) bool
```

```go
// internal/c2c/keys
package keys

type Root struct {
    Name    string          // "gematik.RCA3" / "gematik.RCA-TEST.5" etc.
    Alg     cvcert.KeyAlg
    Key     any             // *rsa.PublicKey or *brainpool.Point
    Source  string          // URL / hash where the key was extracted
}

func ProductionRoots() []Root
func TestRoots() []Root

// VerifyChain walks the certs from leaf → root, ECDSA/RSA-verifying each
// signature against the next CAR/CHR until a trusted Root matches.
func VerifyChain(chain []*cvcert.Cert, roots []Root, at time.Time) error
```

```go
// internal/c2c/sm
package sm

// Session bundles the derived keys + send-sequence counter for one secure
// messaging session.
type Session struct {
    KEnc []byte // AES-128 or AES-256 encryption key
    KMac []byte // AES-128 or AES-256 MAC key
    SSC  []byte // send-sequence counter (8 or 16 bytes)
}

// Wrap encrypts cmdData + computes MAC over the protected header + cmd
// fields per gemSpec_COS §10. Returns the SM APDU ready to Transmit.
func (s *Session) Wrap(cla, ins, p1, p2 byte, cmdData []byte, le int) ([]byte, error)

// Unwrap takes a protected response (data + SW), verifies the MAC,
// decrypts the data, returns plaintext data + SW.
func (s *Session) Unwrap(resp []byte) (data []byte, sw uint16, err error)
```

## Out of scope for this PR

- Live C2C against the user's real eGK (slot-2 test SMC-B's CV-certs chain to TEST root; eGK rejects).
- PIN entry on the terminal keypad (PERFORM VERIFICATION) — that's behind the AllowPINWrite guardrail and a separate user-driven flow.
- C2C against HBA (only SMC-B is required for the eGK-protected reads we care about).

## References

- gemSpec_COS (current): card operating system spec — §10 Secure Messaging, §13 (auth schemes), Anhang B (ATR), Anhang C (Krypt test vectors).
- gemSpec_PKI: PKI deployment, CV-cert formats, root-key publication.
- BSI TR-03110 Part 3 §C.1: CV-cert profile (BSI EAC).
- RFC 5639: Brainpool curves.
- RFC 6979: ECDSA test vectors.
- NIST SP 800-38B: AES-CMAC.
- ISO 7816-4 §6: Secure Messaging.

## Validation strategy

- `cvcert`: parse the real CV-cert files we'll extract from the slot-2 SMC-B (under DF.SMA or DF.ESIGN — TBD during integration).
- `brainpool`: RFC 6979 §A.2.5 vectors + cross-check with a known-good Brainpool implementation.
- `keys`: verify the chain `slot-2 SMC-B X.509 cert → GEM.SMCB-CA24 TEST-ONLY → GEM.RCA TEST-ONLY` succeeds against test roots; fails against production roots.
- `sm`: gemSpec_COS Anhang C test vectors if available, otherwise constructed pairs.
- End-to-end: a stubbed two-card transcript that exercises the handshake step-by-step.
