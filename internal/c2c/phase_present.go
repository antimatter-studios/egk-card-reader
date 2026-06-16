package c2c

// Phase 3 — PresentToVerifier.
//
// After phase 2 validated the SMC-B's CV-cert chain locally, this phase
// walks the chain on the wire and pushes each link into the eGK's
// volatile trust list via the gemSpec_COS §13 / BSI TR-03110 §B.11
// MSE SET DST + PSO VERIFY CERTIFICATE pair.
//
// Order: root-most cert first, leaf-most cert last. The eGK already
// trusts its on-card CVC-Root (matched by h.matchedRoot); each PSO
// VERIFY CERTIFICATE step verifies the next cert against the previously
// imported key, and on success imports its public key. After the final
// PSO VERIFY CERTIFICATE the eGK trusts the SMC-B's AUT_CVC public key,
// which phase 4 then challenges via INTERNAL/EXTERNAL AUTHENTICATE.
//
// Per gemSpec_COS §13.2 + BSI TR-03110-3 §B.11.5, the APDU shapes are:
//
//   MSE SET DST:
//     CLA INS P1 P2 Lc        Data
//     00  22  81 B6 LL        83 LL <CAR>
//
//   PSO VERIFY CERTIFICATE:
//     CLA INS P1 P2 Lc        Data
//     00  2A  00 BE LL        <7F4E body || 5F37 sig>
//
// We send the inner body || signature (not the outer 7F21 wrapper) —
// gemSpec_COS table 240 explicitly specifies the concatenated 7F4E + 5F37
// as the PSO VERIFY CERTIFICATE payload, not the full 7F21 envelope.
//
// SW handling:
//   9000          accepted, key imported, continue
//   6300          authentication failed — the cert did not verify
//                 against the eGK's current trust. Surface as a typed
//                 *c2c.Error so callers can distinguish from I/O failure.
//   6982          security status not satisfied — eGK is in the wrong
//                 lifecycle state. Surface clearly.
//   anything else surfaced as a generic SW error.

import (
	"fmt"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/cvcert"
)

// phasePresentToVerifier runs MSE SET DST + PSO VERIFY CERTIFICATE for each
// cert in the SMC-B chain, root-most first → leaf-most last.
func (h *Handshake) phasePresentToVerifier() error {
	if len(h.smcbChain) == 0 {
		return &Error{
			Phase: PhasePresentToVerifier,
			Role:  RoleSMCB,
			Msg:   "SMC-B chain is empty; PhaseDiscover must run first",
		}
	}
	if h.matchedRoot == nil {
		return &Error{
			Phase: PhasePresentToVerifier,
			Role:  RoleSMCB,
			Msg:   "no trusted CVC-Root matched; PhaseValidateChain must succeed first",
		}
	}

	// smcbChain is leaf-first per the keys.VerifyChain convention
	// (chain[0]=leaf, chain[n-1]=closest-to-root). The eGK expects
	// presentation root-most first, so iterate in reverse.
	for i := len(h.smcbChain) - 1; i >= 0; i-- {
		c := h.smcbChain[i].Cert
		if c == nil {
			return &Error{
				Phase: PhasePresentToVerifier,
				Role:  RoleSMCB,
				Msg:   fmt.Sprintf("chain index %d has nil Cert", i),
			}
		}

		// MSE SET DST: tell the eGK which public key (by CAR == issuer
		// CHR) it should use to verify the next PSO VERIFY CERTIFICATE.
		// chain[i].CAR is the raw 8-byte CHR string as encoded in the
		// cert's tag-42 field. For i = n-1 this is the matched root's
		// CHR; for i < n-1 this is the cert at i+1's CHR.
		if err := mseSetDST(h.opts.EGK, []byte(c.CAR)); err != nil {
			return err
		}

		// PSO VERIFY CERTIFICATE: present the cert body + signature.
		// gemSpec_COS table 240 specifies the payload as the inner
		// `7F4E body || 5F37 sig` — not the outer 7F21 wrapper.
		payload := buildPSOPayload(c)
		if err := psoVerifyCertificate(h.opts.EGK, payload); err != nil {
			return err
		}
	}
	return nil
}

// buildPSOPayload concatenates the 7F4E body TLV and the 5F37 signature
// TLV. cvcert.Cert.Body is the full 7F4E TLV (including its tag and
// length) and Signature is the value bytes of 5F37 — we wrap the signature
// in a fresh 5F37 TLV with a proper length encoding.
func buildPSOPayload(c *cvcert.Cert) []byte {
	out := make([]byte, 0, len(c.Body)+4+len(c.Signature))
	out = append(out, c.Body...)
	out = append(out, encodeSigTLV(c.Signature)...)
	return out
}

// encodeSigTLV builds the 5F37 TLV for a raw signature value. Length
// follows BER short/long form rules.
func encodeSigTLV(sig []byte) []byte {
	hdr := []byte{0x5F, 0x37}
	hdr = append(hdr, encodeLen(len(sig))...)
	return append(hdr, sig...)
}

// encodeLen returns a BER length encoding.
// Signatures are at most a few hundred bytes (RSA-2048 = 256, Brainpool
// P512 = 128) so 82 LL LL is the upper bound used.
func encodeLen(n int) []byte {
	switch {
	case n < 0x80:
		return []byte{byte(n)}
	case n <= 0xFF:
		return []byte{0x81, byte(n)}
	case n <= 0xFFFF:
		return []byte{0x82, byte(n >> 8), byte(n & 0xFF)}
	default:
		// CV-cert signatures never exceed 2 bytes of length; surface a
		// clear panic during development rather than silently truncating.
		panic(fmt.Sprintf("c2c: signature length %d exceeds 2-byte BER", n))
	}
}

// mseSetDST issues the MSE SET DST APDU and maps the SW to a phase 3
// error. car is the raw 8-byte CHR of the public key the verifier
// should use for the upcoming PSO VERIFY CERTIFICATE.
func mseSetDST(card Card, car []byte) error {
	// 83 LL <car>
	dataObj := append([]byte{0x83, byte(len(car))}, car...)
	apdu := append([]byte{0x00, 0x22, 0x81, 0xB6, byte(len(dataObj))}, dataObj...)
	_, sw, err := transmit(card, apdu)
	if err != nil {
		return &Error{
			Phase: PhasePresentToVerifier,
			Role:  RoleEGK,
			Cause: err,
			Msg:   "MSE SET DST: I/O error",
		}
	}
	return classifyPresentSW(sw, "MSE SET DST")
}

// psoVerifyCertificate issues the PSO VERIFY CERTIFICATE APDU.
func psoVerifyCertificate(card Card, payload []byte) error {
	// CLA INS P1 P2 Lc <data>. Lc is 1 byte for < 256; for longer
	// payloads we use extended-length form (00 LL_hi LL_lo).
	var apdu []byte
	if len(payload) < 0x100 {
		apdu = append([]byte{0x00, 0x2A, 0x00, 0xBE, byte(len(payload))}, payload...)
	} else {
		// Extended length: 00 followed by 2-byte big-endian Lc.
		apdu = append([]byte{0x00, 0x2A, 0x00, 0xBE, 0x00, byte(len(payload) >> 8), byte(len(payload) & 0xFF)}, payload...)
	}
	_, sw, err := transmit(card, apdu)
	if err != nil {
		return &Error{
			Phase: PhasePresentToVerifier,
			Role:  RoleEGK,
			Cause: err,
			Msg:   "PSO VERIFY CERTIFICATE: I/O error",
		}
	}
	return classifyPresentSW(sw, "PSO VERIFY CERTIFICATE")
}

// classifyPresentSW maps an ISO 7816 status word to either nil (9000) or
// a typed phase 3 error. step is a short label embedded in the message
// (e.g. "MSE SET DST", "PSO VERIFY CERTIFICATE") so the caller can tell
// which APDU failed.
func classifyPresentSW(sw uint16, step string) error {
	switch sw {
	case 0x9000:
		return nil
	case 0x6300:
		return &Error{
			Phase: PhasePresentToVerifier,
			Role:  RoleEGK,
			Msg:   step + " rejected by eGK (SW=6300 authentication failed)",
		}
	case 0x6982:
		return &Error{
			Phase: PhasePresentToVerifier,
			Role:  RoleEGK,
			Msg:   step + " rejected by eGK (SW=6982 security status not satisfied)",
		}
	default:
		return &Error{
			Phase: PhasePresentToVerifier,
			Role:  RoleEGK,
			Msg:   fmt.Sprintf("%s: unexpected SW=%04X", step, sw),
		}
	}
}
