# C2C — Card-to-Card Authentication

This directory documents the implementation of **gemSpec_COS chapter 13**
card-to-card mutual authentication on the German eGK. It's the body of
work that unlocks PIN-protected segments (NFD, DPE, eMP, …) of an eGK
when paired with a valid SMC-B in a dual-slot terminal.

The Go implementation lives at [`internal/c2c/`](../../internal/c2c/) and
its subpackages (`cvcert`, `brainpool`, `keys`, `sm`, plus the orchestrator
in `handshake.go` + per-phase files).

## Status

| # | Wall | State |
|---|------|-------|
| 1 | **Driver** — ORGA terminal usable on macOS | ✅ Done — see [../orga-driver/](../orga-driver/) |
| 2 | **SMC-B with valid CV-certs** | ❌ Blocked — see [walls.md](walls.md) |
| 3 | **Cardholder PIN** | 🟡 In hand, blocked behind safety guard — see [pin-workflow.md](pin-workflow.md) |
| 4 | **gemSpec_COS C2C in code** | ✅ Done — all 5 phases implemented + unit-tested |

See [walls.md](walls.md) for the full four-walls model and why Wall 2
blocks every other wall's value.

## Reading order

1. [walls.md](walls.md) — the four-walls model: what has to be true for
   PIN-protected reads to work, and why.
2. [plan.md](plan.md) — the implementation plan (now mostly retrospective)
   and design contracts for each subpackage.
3. [pin-workflow.md](pin-workflow.md) — where the PIN comes from
   (Krankenkasse / TK), counter math, and the safety mechanics around
   VERIFY in this codebase.
4. [cvc-root-research.md](cvc-root-research.md) — provenance log for the
   gematik CVC-Root public keys we embed in
   [`internal/c2c/keys/cvc_roots.go`](../../internal/c2c/keys/cvc_roots.go).
5. [slot2-no-cvcerts.md](slot2-no-cvcerts.md) — finding that the user's
   slot-2 test SMC-B carries only X.509, no CV-certs; the live-test wall.
6. [probes/](probes/) — captured outputs of `cmd/orga-probe -c2c <slot>`
   against the test SMC-B with each root set.

## Five-phase handshake architecture

The `Handshake` orchestrator in
[`internal/c2c/handshake.go`](../../internal/c2c/handshake.go) drives:

```
1. DiscoverPeerCerts    → read SMC-B CV-cert chain via FID sweep
                          (discover.go, KnownSMCBCertSlots)
2. ValidatePeerChain    → verify chain locally against gematik CVC-Roots
                          (keys.VerifyChain + brainpool ECDSA verify)
3. PresentToVerifier    → MSE SET DST + PSO VERIFY CERTIFICATE for each
                          link, root-most first → leaf-most last
                          (phase_present.go)
4. MutualAuthenticate   → MSE SET AT / GET CHALLENGE on eGK,
                          INTERNAL AUTHENTICATE on SMC-B,
                          EXTERNAL AUTHENTICATE on eGK
                          (phase_mutual.go)
5. OpenSecureChannel    → gemSpec_Krypt Algorithm-2 KDF
                          (SHA-1(K || counter)[:16]) → *sm.Session
                          (phase_secure.go)
```

After phase 5 the host holds a `*sm.Session` (in
[`internal/c2c/sm/`](../../internal/c2c/sm/)) ready to wrap subsequent
APDUs — VERIFY PIN, READ BINARY on protected EFs — with AES-128 CBC
encryption and AES-CMAC integrity per gemSpec_COS §10.

## Driving against a live card

```
# Activate slot 2 (recovers a TCOS card from a "Fatal Error 3" stale state)
./orga-probe -slot 0 -apdu "20 12 02 01 00"

# Run the C2C probe against the test SMC-B with TEST roots
./orga-probe -c2c 2 -c2c-test-roots=true -c2c-out docs/c2c/probes/run.md

# Same, but against production roots (expected: untrusted-chain)
./orga-probe -c2c 2 -c2c-test-roots=false -c2c-out docs/c2c/probes/run-prod.md
```

The probe runs phases 1+2 only by design (phases 3-5 are scaffolded but
require a live CV-cert-bearing SMC-B to exercise). See `cmd/orga-probe/c2c.go`.
