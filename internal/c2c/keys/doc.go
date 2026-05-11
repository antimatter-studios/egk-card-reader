// Package keys holds the gematik PKI root public keys (production and
// TEST-ONLY) used to anchor card-to-card (C2C) certificate-chain
// verification per gemSpec_COS chapter 13 / gemSpec_PKI.
//
// # Scope
//
// This package exposes:
//
//   - Two slices of trust anchors, ProductionRoots and TestRoots, each
//     element a Root carrying the parsed public key plus provenance
//     metadata (source URL, SHA-256 fingerprint of the source certificate,
//     subject DN, validity).
//   - VerifyChain, which walks a slice of *cvcert.Cert from leaf to root,
//     verifying each link's signature and that the time `at` lies within
//     every certificate's NotBefore/NotAfter, and finally that the chain
//     terminates at a Root in the supplied trust set.
//
// # Root-key provenance
//
// All keys were fetched in May 2026 from gematik's public download
// endpoints. See sources.md in this directory for full URLs, SHA-256
// checksums (cross-checked against gematik's own .sha256 files where
// available) and dates fetched.
//
//   - Production roots: https://download.tsl.ti-dienste.de/ROOT-CA/
//   - Test roots:       https://download-test.tsl.ti-dienste.de/ROOT-CA/
//
// At the time of fetch three generations of each were publicly available:
// GEM.RCA2 (legacy, valid 2016-12 → 2026-12), GEM.RCA6 (current production
// for X.509 TI certificates, valid 2021-11 → 2031-11) and GEM.RCA9
// (next generation, valid 2025-06 → 2035-06). All are RSA-2048,
// SHA-256-with-RSA self-signed. The corresponding TEST-ONLY roots
// (used by gematik's test PKI; not trusted by real cards) carry CN
// suffixed with " TEST-ONLY" and the O= " gematik GmbH NOT-VALID".
//
// The slot-2 SMC-B X.509 cert in
// docs/orga-driver/cards/slot2-certs/sfi1-c500.pem (issuer
// "GEM.SMCB-CA24 TEST-ONLY", AKI 7AE9E1...D99E) chains through
// GEM.SMCB-CA24 TEST-ONLY to GEM.RCA2 TEST-ONLY (the SMCB-CA24
// intermediate is listed in the TEST-PKI TSL).
//
// # Relationship to other c2c subpackages
//
// VerifyChain operates on *cvcert.Cert values produced by
// internal/c2c/cvcert. RSA-2048 signatures are verified with
// crypto/rsa.VerifyPKCS1v15; Brainpool ECDSA signatures are verified
// via internal/c2c/brainpool.
//
// # gemSpec_COS chapter 13 caveat
//
// The CV-certs presented by the eGK during the C2C handshake terminate
// at a separate gematik CVC-Root (a parallel PKI to the X.509 TSL).
// gematik publishes CVC-Root keys via gemSpec_PKI / gemSpec_CVC_Root,
// not via the TSL endpoints. The CVC-Root keys required for true
// card-to-card authentication against a real eGK are NOT embedded here
// yet — see the TODO entries in roots.go and sources.md. For the
// current milestone the test SMC-B X.509 chain (which uses the same
// gematik test-PKI roots) is sufficient to exercise the VerifyChain
// state machine.
package keys
