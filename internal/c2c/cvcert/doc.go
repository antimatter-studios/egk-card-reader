// Package cvcert parses Card-Verifiable (CV) certificates used for
// Card-to-Card (C2C) authentication on German healthcare cards
// (eGK / SMC-B / HBA).
//
// CV-certificates are NOT X.509. They are BER-TLV structures defined in:
//
//   - BSI TR-03110 Part 3, Annex C.1 "Card Verifiable Certificates"
//   - gemSpec_PKI (gematik PKI spec) — profile of the BSI structure
//   - gemSpec_COS chapter 13 — usage in C2C authentication
//
// Wire layout (outer-most tag first):
//
//	7F21 — CV Certificate
//	  7F4E — Certificate Body (the part covered by the signature)
//	    5F29 — Certificate Profile Indicator (1 byte)
//	    42   — Certificate Authority Reference (CAR), ASCII
//	    7F49 — Public Key
//	      06 — OID identifying the algorithm
//	      ...key material (RSA: 81/82; ECDSA: 86 uncompressed point)
//	    5F20 — Certificate Holder Reference (CHR), ASCII
//	    7F4C — Certificate Holder Authorisation Template (CHAT) [optional]
//	    5F25 — Effective Date (6 byte BCD YYMMDD)
//	    5F24 — Expiration Date (6 byte BCD YYMMDD)
//	  5F37 — Signature value (ECDSA r||s, or RSA signature)
//
// This package contains only parsing: no signature verification, no curve
// math, no card I/O. Signature verification and key-graph traversal live in
// the sibling packages internal/c2c/keys and internal/c2c/bp.
package cvcert
