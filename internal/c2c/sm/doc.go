// Package sm implements gemSpec_COS Secure Messaging wrap/unwrap for ISO
// 7816-4 APDUs as used on the German electronic health card (eGK) after a
// successful Card-to-Card (C2C) handshake.
//
// # Background
//
// Once the C2C handshake has established symmetric session keys
// (K_ENC, K_MAC) and an initial Send-Sequence-Counter (SSC), every command
// APDU sent to the card and every response APDU returned by the card is
// transformed into a "Secure Messaging" APDU per ISO 7816-4 §6 with the
// gematik-specific profile defined in gemSpec_COS chapter 10
// ("Secure Messaging"). Confidentiality is provided by AES in CBC mode and
// authenticity/integrity by AES-CMAC (NIST SP 800-38B) truncated to 8 bytes.
//
// # Data-object layout
//
// BER-TLV, single-byte tags. Lengths use the shortest legal encoding
// (0..127 → one byte; 128..255 → 81 LL; 256..65535 → 82 LL LL):
//
//	'87'  Padding-content indicator (0x01 = ISO 7816-4 padding mode 2) ||
//	      encrypted command/response data.
//	'97'  Expected response length Le (1 or 2 bytes, short form here).
//	'99'  Protected status word (exactly 2 bytes, plaintext under gemSpec_COS).
//	'8E'  Cryptographic checksum: 8-byte truncated AES-CMAC.
//
// Command DO order: '87' (if data present), '97' (if Le requested), '8E'.
// Response DO order: '87' (optional), '99', '8E'.
//
// # Header
//
// The protected command header sets CLA bits 0x0C ("SM with header
// authentication, ISO formatting"). We compute it as
// (cla & 0xFC) | 0x0C per gemSpec_COS_v2.
//
// # MAC scope
//
// Command direction:
//
//	SSC || pad( CLA INS P1 P2 ) || pad( all DOs except '8E' in order )
//
// Response direction:
//
//	SSC || pad( all DOs except '8E' in order )
//
// where pad() is ISO/IEC 7816-4 padding mode 2 to the AES block size (16
// bytes). The concatenation is padded once more if needed before CMAC; for
// the command direction the protected header is padded to a block boundary
// in isolation first, matching gemSpec_COS test-vector practice.
//
// The SSC is a 16-byte big-endian counter that is incremented by one
// *before* each MAC computation. Both Wrap and Unwrap advance the counter,
// so a host that wraps a command and unwraps the matching response moves
// the counter by +2 per round-trip — mirroring the card.
//
// # CBC IV
//
// gemSpec_COS computes the SM IV as IV = AES_KEnc(SSC) so that the regular
// counter-like prefix of plaintext blocks is masked. Wrap and Unwrap both
// use this IV after the (already-incremented) SSC.
//
// # Spec ambiguities resolved
//
//   - When the cmdData is exactly block-aligned, ISO 7816-4 mode 2 padding
//     still appends a full 16-byte padding block. This package always
//     applies that rule (the standard foot-gun).
//   - "Pad the protected header in isolation, then concatenate, then pad
//     again": gemSpec_COS_v2 §10 worked example does it this way. We follow
//     that even though several v1-era informal references inline the
//     header into a single concatenated pad. The v2 form is the chosen
//     convention.
//   - Le encoding: short form (single byte) is used for le in [0,255].
//     Wrap rejects values outside that range; gemSpec_COS C2C never sends
//     extended Le for SM-wrapped commands.
//
// # References
//
//   - gemSpec_COS (gematik), chapter 10 "Secure Messaging"
//   - ISO/IEC 7816-4:2013, §6 "Secure messaging"
//   - NIST SP 800-38B, "Recommendation for Block Cipher Modes of Operation:
//     The CMAC Mode for Authentication" (Appendix D vectors used in tests)
//   - NIST SP 800-38A, "… Methods and Techniques" (CBC mode)
//
// # Scope and limitations
//
//   - AES-128 and AES-256 keys are supported (K_ENC and K_MAC must share
//     the same length). AES-192 is accepted by the underlying crypto/aes
//     but is not exercised by gemSpec_COS C2C. 3DES SM is *not* implemented.
//   - Le is encoded in the short form only.
//   - The package is independent of the rest of internal/c2c/* — it only
//     uses the Go standard library.
package sm
