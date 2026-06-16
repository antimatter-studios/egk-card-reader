package c2c

// Phase 5 — OpenSecureChannel.
//
// Derives the AES Secure-Messaging session keys K_ENC and K_MAC and the
// initial Send-Sequence-Counter (SSC) from the two nonces gathered during
// mutual authentication (phase 4), then instantiates a *sm.Session ready
// for protected APDUs.
//
// Pure crypto — no card I/O happens in this phase.
//
// Derivation citation
// -------------------
// gemSpec_Krypt "Algorithm-2" (also called "Krypt 2.0") specifies the
// key-derivation function used for the gemSpec_COS C2C handshake:
//
//	K_ENC = first 16 bytes of SHA-1( K || 0x00 0x00 0x00 0x01 )
//	K_MAC = first 16 bytes of SHA-1( K || 0x00 0x00 0x00 0x02 )
//
// where K is the concatenation of the two nonces exchanged during phase 4
// (host random || card random). The SSC starts at all-zeros (one AES block,
// i.e. 16 bytes).
//
// The use of SHA-1 here — while the rest of gemSpec is SHA-256+ — is an
// explicit choice of the spec: gemSpec_Krypt Algorithm-2 was lifted from
// the BSI TR-03110 PACE / Chip-Authentication tradition where SHA-1 is
// the truncation primitive for AES-128 key derivation. SHA-256 derivations
// are reserved for AES-256 (Algorithm-3) which we do not implement in this
// iteration (see "Scope" below).
//
// Scope
// -----
// This implementation supports the "AES-128" negotiated algorithm only.
// AES-256 (negotiatedAlg "AES-256") would use Algorithm-3 with SHA-256 and
// a 32-byte key truncation; it's deferred until we have a test card that
// negotiates it, since the slot-2 SMC-B we develop against is TEST-ONLY
// and only supports AES-128 today.

import (
	"crypto/sha1" //nolint:gosec // gemSpec_Krypt Algorithm-2 mandates SHA-1 for KDF
	"encoding/binary"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/sm"
)

// aes128KeyLen is the truncation length for K_ENC and K_MAC under
// gemSpec_Krypt Algorithm-2 (AES-128).
const aes128KeyLen = 16

// sscInitLen is the length of the initial Send-Sequence-Counter — one AES
// block, all zeros — per gemSpec_COS §10.2.
const sscInitLen = 16

func (h *Handshake) phaseOpenSecureChannel() error {
	// Precondition checks. Phase 4 must have populated the scratchpad.
	if h.state.nonceHost == nil {
		return &Error{
			Phase: PhaseOpenSecure,
			Msg:   "phase 4 state incomplete: nonceHost is nil",
		}
	}
	if h.state.nonceCard == nil {
		return &Error{
			Phase: PhaseOpenSecure,
			Msg:   "phase 4 state incomplete: nonceCard is nil",
		}
	}
	if h.state.negotiatedAlg == "" {
		return &Error{
			Phase: PhaseOpenSecure,
			Msg:   "phase 4 state incomplete: negotiatedAlg is empty",
		}
	}

	switch h.state.negotiatedAlg {
	case "AES-128":
		// Supported below.
	default:
		return &Error{
			Phase: PhaseOpenSecure,
			Msg:   "negotiated algorithm not supported: " + h.state.negotiatedAlg,
		}
	}

	// Build the base secret K from the concatenated nonces. The standard
	// gemSpec_COS C2C nonces are 16 bytes each, but we don't enforce a
	// specific length here — Algorithm-2 itself only fixes the trailing
	// counter and the output truncation; the input length is determined
	// by the protocol.
	k := make([]byte, 0, len(h.state.nonceHost)+len(h.state.nonceCard))
	k = append(k, h.state.nonceHost...)
	k = append(k, h.state.nonceCard...)

	kEnc := deriveKeyAlg2(k, 1)
	kMac := deriveKeyAlg2(k, 2)

	h.session = &sm.Session{
		KEnc: kEnc,
		KMac: kMac,
		SSC:  make([]byte, sscInitLen), // all-zero SSC; first Wrap/Unwrap increments
	}
	return nil
}

// deriveKeyAlg2 implements one step of gemSpec_Krypt Algorithm-2 for an
// AES-128 session:
//
//	output = SHA-1(K || BE32(counter))[:16]
//
// counter is 1 for K_ENC, 2 for K_MAC. The trailing 4-byte big-endian
// counter is appended to K verbatim — there is no separator byte. The
// SHA-1 output is truncated to the first 16 bytes (AES-128 key length).
func deriveKeyAlg2(k []byte, counter uint32) []byte {
	buf := make([]byte, len(k)+4)
	copy(buf, k)
	binary.BigEndian.PutUint32(buf[len(k):], counter)
	sum := sha1.Sum(buf) //nolint:gosec // KDF, not a hash-for-signatures
	out := make([]byte, aes128KeyLen)
	copy(out, sum[:aes128KeyLen])
	return out
}
