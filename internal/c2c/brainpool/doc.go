// Package brainpool implements the RFC 5639 Brainpool elliptic curves
// (brainpoolP256r1, brainpoolP384r1, brainpoolP512r1) and ECDSA signature
// verification on them.
//
// Go's standard library crypto/elliptic package does not include the
// Brainpool curves. These curves are required for German healthcare-card
// "Card-to-Card" (C2C) authentication per gemSpec_COS chapter 13, which
// mandates Brainpool P256r1 / P384r1 / P512r1 for ECDSA. The SMC-B
// certificate in slot 2 (C.SMC.AUT_R2048) carries Brainpool P256r1 ECDSA
// at C506.
//
// Scope of this package:
//   - Curve parameter singletons (from RFC 5639 §3.4, §3.6, §3.7).
//   - Affine point arithmetic: Add, Double, ScalarMult, ScalarBaseMult.
//   - ECDSA signature verification (SEC 1 §4.1.4).
//
// Not in scope:
//   - Card I/O (handled elsewhere).
//   - CV certificate parsing (separate subpackage).
//   - Constant-time scalar multiplication. The verify path operates on
//     attacker-supplied public inputs only; we optimize for clarity.
//     Do NOT use this package for signing with a private key — there is
//     no signing implementation, and the arithmetic is not side-channel
//     hardened.
//
// References:
//   - RFC 5639  "Elliptic Curve Cryptography (ECC) Brainpool Standard
//     Curves and Curve Generation" (March 2010).
//   - SEC 1 v2.0 "Elliptic Curve Cryptography" §4.1.4 (signature
//     verification).
//   - BSI TR-03111 v2.10 "Elliptic Curve Cryptography" Appendix D
//     (Brainpool P256r1 ECDSA test vectors).
//   - gemSpec_COS v3.13.0 chapter 13 (C2C authentication).
package brainpool
