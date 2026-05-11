package c2c

import (
	"crypto/rand"
	"errors"
	"fmt"
)

// phaseMutualAuth runs Direction 1 of the gemSpec_COS §13 mutual
// authentication: the eGK challenges the SMC-B, the host carries the
// challenge over to the SMC-B for signing, and the eGK verifies the
// returned signature.
//
// Preconditions (asserted in this order):
//   - h.smcbChain is non-empty (phases 1-3 ran and imported the SMC-B
//     pubkey into the eGK's trust list).
//   - h.opts.EGK and h.opts.SMCB are both non-nil.
//
// On success h.state.nonceHost / nonceCard / negotiatedAlg are populated
// for phase 5 to derive Secure Messaging session keys.
//
// APDU choreography (host as MITM between the two cards):
//
//  1. eGK ← MSE SET AT (00 22 81 A4 …)             bind SMC-B as auth peer
//  2. eGK → GET CHALLENGE (00 84 00 00 08)         eGK's nonce (8 bytes)
//  3. SMCB → INTERNAL AUTHENTICATE (00 88 00 00 …) signs the eGK nonce
//  4. eGK ← EXTERNAL AUTHENTICATE (00 82 00 00 …)  eGK verifies the sig
//
// Spec citations are inline below.
func (h *Handshake) phaseMutualAuth() error {
	// --- Precondition checks (must run before any APDU is sent). ---
	if len(h.smcbChain) == 0 {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleHost,
			Msg:   "SMC-B CV-cert chain empty; phases 1-3 must run first",
			Cause: errors.New("len(h.smcbChain) == 0"),
		}
	}
	if h.opts.EGK == nil {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleEGK,
			Msg: "EGK transport not configured", Cause: errors.New("Options.EGK is nil"),
		}
	}
	if h.opts.SMCB == nil {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleSMCB,
			Msg: "SMC-B transport not configured", Cause: errors.New("Options.SMCB is nil"),
		}
	}

	// The SMC-B's CHR is the binding identifier the eGK just imported into
	// its trust list in phase 3 (PSO VERIFY CERTIFICATE on the leaf cv-cert).
	// h.smcbChain[0] is the leaf per the keys.VerifyChain convention.
	smcbCHR := []byte(h.smcbChain[0].Cert.CHR)
	if len(smcbCHR) == 0 || len(smcbCHR) > 0xFF {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleHost,
			Msg:   "SMC-B CHR has illegal length",
			Cause: fmt.Errorf("len(CHR)=%d (want 1..255)", len(smcbCHR)),
		}
	}

	// --- Step 1: MSE SET AT on eGK ------------------------------------
	//
	// gemSpec_COS §14.7.1.1 (MANAGE SECURITY ENVIRONMENT) and §13.4.4
	// (use in C2C): CLA=00 INS=22 P1=81 (SET) P2=A4 (Authentication
	// template). Data is a CRT containing tag 83 (Key reference / CHR
	// of the external entity to be authenticated). See ISO 7816-4 §7.5.11.
	//
	//   APDU: 00 22 81 A4 Lc 83 LL <SMCB_CHR>
	mseSetAT := buildShortAPDU(0x00, 0x22, 0x81, 0xA4, encodeTLVByte(0x83, smcbCHR))
	_, sw, err := transmit(h.opts.EGK, mseSetAT)
	if err != nil {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleEGK,
			Msg: "MSE SET AT transport failure", Cause: err,
		}
	}
	if sw != 0x9000 {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleEGK,
			Msg:   fmt.Sprintf("MSE SET AT rejected (SW=%04X)", sw),
			Cause: fmt.Errorf("SW=%04X", sw),
		}
	}

	// --- Step 2: GET CHALLENGE on eGK ----------------------------------
	//
	// gemSpec_COS §14.9.2 / ISO 7816-4 §7.5.3: CLA=00 INS=84 P1=00 P2=00
	// Le = 8 (8-byte challenge for Brainpool P-256 + Algorithm-2).
	//
	//   APDU: 00 84 00 00 08
	getChallenge := []byte{0x00, 0x84, 0x00, 0x00, 0x08}
	rndEGK, sw, err := transmit(h.opts.EGK, getChallenge)
	if err != nil {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleEGK,
			Msg: "GET CHALLENGE failed", Cause: err,
		}
	}
	if sw != 0x9000 {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleEGK,
			Msg:   "GET CHALLENGE failed",
			Cause: fmt.Errorf("SW=%04X", sw),
		}
	}
	if len(rndEGK) != 8 {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleEGK,
			Msg:   "GET CHALLENGE returned wrong length",
			Cause: fmt.Errorf("got %d bytes, want 8", len(rndEGK)),
		}
	}

	// --- Step 3: generate RND_host -------------------------------------
	//
	// gemSpec_COS §13.4: the host contribution to the protocol-3 input
	// is 16 random bytes. Phase 5 truncates per the negotiated key length;
	// here we pass the full 16-byte nonce into INTERNAL AUTHENTICATE.
	rndHost := make([]byte, 16)
	if _, err := rand.Read(rndHost); err != nil {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleHost,
			Msg: "RNG read failed", Cause: err,
		}
	}

	// --- Step 4: INTERNAL AUTHENTICATE on SMC-B ------------------------
	//
	// gemSpec_COS §14.7.4.1 / ISO 7816-4 §7.5.10. CLA=00 INS=88 P1=00 P2=00
	// Lc data Le=00. The SMC-B signs SHA-256(RND_eGK || RND_host ||
	// PuK_eGK_marker) internally; the host only ships the byte input.
	// The PuK marker is implicit on the SMC-B from the MSE step it ran
	// earlier (out of scope for Direction 1).
	//
	//   APDU: 00 88 00 00 Lc <RND_eGK || RND_host> 00
	iaData := append([]byte(nil), rndEGK...)
	iaData = append(iaData, rndHost...)
	internalAuth := append(buildShortAPDU(0x00, 0x88, 0x00, 0x00, iaData), 0x00)
	smcbSig, sw, err := transmit(h.opts.SMCB, internalAuth)
	if err != nil {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleSMCB,
			Msg: "INTERNAL AUTHENTICATE failed", Cause: err,
		}
	}
	if sw != 0x9000 {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleSMCB,
			Msg:   "INTERNAL AUTHENTICATE failed",
			Cause: fmt.Errorf("SW=%04X", sw),
		}
	}
	if len(smcbSig) == 0 {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleSMCB,
			Msg:   "INTERNAL AUTHENTICATE returned empty signature",
			Cause: errors.New("zero-length response body"),
		}
	}

	// --- Step 5: EXTERNAL AUTHENTICATE on eGK --------------------------
	//
	// gemSpec_COS §14.7.4.2 / ISO 7816-4 §7.5.5. CLA=00 INS=82 P1=00 P2=00
	// Lc data. The eGK verifies smcbSig against the public key imported
	// in phase 3.
	//
	//   APDU: 00 82 00 00 Lc <smcb_signature_plus_meta>
	extAuth := buildShortAPDU(0x00, 0x82, 0x00, 0x00, smcbSig)
	_, sw, err = transmit(h.opts.EGK, extAuth)
	if err != nil {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleEGK,
			Msg: "EXTERNAL AUTHENTICATE transport failure", Cause: err,
		}
	}
	if sw != 0x9000 {
		return &Error{
			Phase: PhaseMutualAuth, Role: RoleEGK,
			Msg:   fmt.Sprintf("EXTERNAL AUTHENTICATE rejected (SW=%04X)", sw),
			Cause: fmt.Errorf("SW=%04X", sw),
		}
	}

	// --- Populate session state for phase 5 ----------------------------
	h.state.nonceHost = rndHost
	h.state.nonceCard = rndEGK
	// gemSpec_COS Algorithm-2 default for Brainpool P-256r1 cards is AES-128.
	h.state.negotiatedAlg = "AES-128"
	return nil
}

// buildShortAPDU assembles a short-form (Lc <= 255) command APDU. The
// extended-length form is used by phase 3 for long PSO VERIFY CERTIFICATE
// payloads; phase 4's APDUs always fit short.
func buildShortAPDU(cla, ins, p1, p2 byte, data []byte) []byte {
	if len(data) > 0xFF {
		// Defensive: phase 4 never produces this in practice. If a future
		// caller wires in larger data, emit extended-length so the card
		// surfaces the error rather than panicking here.
		out := make([]byte, 0, 7+len(data))
		out = append(out, cla, ins, p1, p2, 0x00, byte(len(data)>>8), byte(len(data)))
		return append(out, data...)
	}
	out := make([]byte, 0, 5+len(data))
	out = append(out, cla, ins, p1, p2, byte(len(data)))
	return append(out, data...)
}

// encodeTLVByte builds a single BER-TLV with a 1-byte tag and short-form
// length (≤127). Used to wrap the SMC-B CHR (tag 83) inside the MSE SET AT
// data. CHR strings on gematik cards are 8–16 ASCII bytes — always short.
func encodeTLVByte(tag byte, value []byte) []byte {
	if len(value) > 0x7F {
		out := make([]byte, 0, 3+len(value))
		out = append(out, tag, 0x81, byte(len(value)))
		return append(out, value...)
	}
	out := make([]byte, 0, 2+len(value))
	out = append(out, tag, byte(len(value)))
	return append(out, value...)
}
