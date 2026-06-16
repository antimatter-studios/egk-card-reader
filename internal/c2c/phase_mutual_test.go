package c2c

import (
	"bytes"
	"errors"
	"testing"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/cvcert"
)

// Phase-4 tests share fakeCard/scriptEntry/mustHex with discover_test.go.
// scriptEntry matches by prefix: shorter `match` slices catch any APDU
// starting with those bytes (first match wins). For phase 4 that lets us
// register one entry per APDU shape regardless of variable trailing data
// (CHR strings, random nonces, signatures).

const smcbCHRFixture = "SMCBCHR1" // 8 bytes

func newPhase4Handshake(t *testing.T, egk, smcb Card) *Handshake {
	t.Helper()
	h, err := New(Options{EGK: egk, SMCB: smcb})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.smcbChain = []DiscoveredCert{{Cert: &cvcert.Cert{CHR: smcbCHRFixture}}}
	return h
}

func TestPhaseMutualAuth_HappyPath(t *testing.T) {
	rndEGK := []byte{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7}
	smcbSig := bytes.Repeat([]byte{0xC0}, 64)

	egk := &fakeCard{scripts: []scriptEntry{
		// MSE SET AT: 00 22 81 A4 Lc 83 LL <CHR>. Match the 4-byte prefix.
		{match: mustHex("00 22 81 A4"), resp: mustHex("9000")},
		// GET CHALLENGE: exact 5-byte APDU
		{match: mustHex("00 84 00 00 08"), resp: append(append([]byte{}, rndEGK...), 0x90, 0x00)},
		// EXTERNAL AUTHENTICATE: prefix 00 82 00 00
		{match: mustHex("00 82 00 00"), resp: mustHex("9000")},
	}}
	smcb := &fakeCard{scripts: []scriptEntry{
		// INTERNAL AUTHENTICATE: prefix 00 88 00 00
		{match: mustHex("00 88 00 00"), resp: append(append([]byte{}, smcbSig...), 0x90, 0x00)},
	}}

	h := newPhase4Handshake(t, egk, smcb)
	if err := h.phaseMutualAuth(); err != nil {
		t.Fatalf("phaseMutualAuth: %v", err)
	}

	if !bytes.Equal(h.state.nonceCard, rndEGK) {
		t.Errorf("nonceCard mismatch: got %X want %X", h.state.nonceCard, rndEGK)
	}
	if len(h.state.nonceHost) != 16 {
		t.Errorf("nonceHost length: got %d want 16", len(h.state.nonceHost))
	}
	if h.state.negotiatedAlg != "AES-128" {
		t.Errorf("negotiatedAlg = %q, want %q", h.state.negotiatedAlg, "AES-128")
	}
}

func TestPhaseMutualAuth_APDUSequence(t *testing.T) {
	rndEGK := bytes.Repeat([]byte{0xB1}, 8)
	smcbSig := bytes.Repeat([]byte{0xC0}, 64)

	egk := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 22 81 A4"), resp: mustHex("9000")},
		{match: mustHex("00 84 00 00 08"), resp: append(append([]byte{}, rndEGK...), 0x90, 0x00)},
		{match: mustHex("00 82 00 00"), resp: mustHex("9000")},
	}}
	smcb := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 88 00 00"), resp: append(append([]byte{}, smcbSig...), 0x90, 0x00)},
	}}

	h := newPhase4Handshake(t, egk, smcb)
	if err := h.phaseMutualAuth(); err != nil {
		t.Fatalf("phaseMutualAuth: %v", err)
	}

	if len(egk.calls) != 3 {
		t.Fatalf("eGK got %d APDUs, want 3", len(egk.calls))
	}
	if len(smcb.calls) != 1 {
		t.Fatalf("SMC-B got %d APDUs, want 1", len(smcb.calls))
	}

	// APDU 1: MSE SET AT — verify CHR was wrapped correctly.
	mseSetAT := egk.calls[0]
	if !bytes.HasPrefix(mseSetAT, []byte{0x00, 0x22, 0x81, 0xA4}) {
		t.Errorf("APDU 1 not MSE SET AT: %X", mseSetAT)
	}
	// Lc field at index 4, then `83 LL <CHR>` at 5..
	if mseSetAT[5] != 0x83 {
		t.Errorf("APDU 1 missing tag 83: %X", mseSetAT)
	}
	chrLen := int(mseSetAT[6])
	if chrLen != len(smcbCHRFixture) {
		t.Errorf("APDU 1 CHR length = %d, want %d", chrLen, len(smcbCHRFixture))
	}
	gotCHR := string(mseSetAT[7 : 7+chrLen])
	if gotCHR != smcbCHRFixture {
		t.Errorf("APDU 1 CHR = %q, want %q", gotCHR, smcbCHRFixture)
	}

	// APDU 2: GET CHALLENGE
	if !bytes.Equal(egk.calls[1], []byte{0x00, 0x84, 0x00, 0x00, 0x08}) {
		t.Errorf("APDU 2 = %X, want GET CHALLENGE", egk.calls[1])
	}

	// APDU 3 (SMC-B): INTERNAL AUTHENTICATE with RND_eGK || RND_host (24 bytes).
	ia := smcb.calls[0]
	if !bytes.HasPrefix(ia, []byte{0x00, 0x88, 0x00, 0x00}) {
		t.Errorf("SMC-B APDU not INTERNAL AUTHENTICATE: %X", ia)
	}
	iaLc := int(ia[4])
	if iaLc != 24 {
		t.Errorf("INTERNAL AUTHENTICATE Lc = %d, want 24 (8+16)", iaLc)
	}
	iaData := ia[5 : 5+iaLc]
	if !bytes.HasPrefix(iaData, rndEGK) {
		t.Errorf("INTERNAL AUTHENTICATE data doesn't start with RND_eGK: %X", iaData)
	}

	// APDU 4: EXTERNAL AUTHENTICATE carrying the SMC-B signature.
	ea := egk.calls[2]
	if !bytes.HasPrefix(ea, []byte{0x00, 0x82, 0x00, 0x00}) {
		t.Errorf("APDU 3 not EXTERNAL AUTHENTICATE: %X", ea)
	}
	eaLc := int(ea[4])
	eaData := ea[5 : 5+eaLc]
	if !bytes.Equal(eaData, smcbSig) {
		t.Errorf("EXTERNAL AUTHENTICATE payload != SMC-B signature")
	}
}

func TestPhaseMutualAuth_EmptyChain(t *testing.T) {
	c := &fakeCard{}
	h, err := New(Options{EGK: c, SMCB: c})
	if err != nil {
		t.Fatal(err)
	}
	// Don't populate h.smcbChain — phase 4 must refuse.
	err = h.phaseMutualAuth()
	if err == nil {
		t.Fatal("expected precondition error")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T", err)
	}
	if ce.Phase != PhaseMutualAuth || ce.Role != RoleHost {
		t.Errorf("Phase=%v Role=%v", ce.Phase, ce.Role)
	}
	if len(c.calls) != 0 {
		t.Errorf("expected no APDUs sent, got %d", len(c.calls))
	}
}

func TestPhaseMutualAuth_MSEFailureShortCircuits(t *testing.T) {
	egk := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 22 81 A4"), resp: mustHex("6982")}, // security status not satisfied
	}}
	smcb := &fakeCard{}

	h := newPhase4Handshake(t, egk, smcb)
	err := h.phaseMutualAuth()
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *Error
	if !errors.As(err, &ce) || ce.Phase != PhaseMutualAuth || ce.Role != RoleEGK {
		t.Errorf("Phase=%v Role=%v err=%v", ce.Phase, ce.Role, err)
	}
	if len(egk.calls) != 1 {
		t.Errorf("eGK should have received exactly 1 APDU before short-circuiting, got %d", len(egk.calls))
	}
	if len(smcb.calls) != 0 {
		t.Errorf("SMC-B should not have been called, got %d", len(smcb.calls))
	}
}

func TestPhaseMutualAuth_GetChallengeFails(t *testing.T) {
	egk := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 22 81 A4"), resp: mustHex("9000")},
		{match: mustHex("00 84 00 00 08"), resp: mustHex("6982")},
	}}
	smcb := &fakeCard{}
	h := newPhase4Handshake(t, egk, smcb)
	err := h.phaseMutualAuth()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %v", err)
	}
	if ce.Phase != PhaseMutualAuth || ce.Role != RoleEGK {
		t.Errorf("Phase=%v Role=%v", ce.Phase, ce.Role)
	}
}

func TestPhaseMutualAuth_InternalAuthFails(t *testing.T) {
	rndEGK := bytes.Repeat([]byte{0xD1}, 8)
	egk := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 22 81 A4"), resp: mustHex("9000")},
		{match: mustHex("00 84 00 00 08"), resp: append(append([]byte{}, rndEGK...), 0x90, 0x00)},
	}}
	smcb := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 88 00 00"), resp: mustHex("6985")},
	}}
	h := newPhase4Handshake(t, egk, smcb)
	err := h.phaseMutualAuth()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %v", err)
	}
	if ce.Phase != PhaseMutualAuth || ce.Role != RoleSMCB {
		t.Errorf("Phase=%v Role=%v", ce.Phase, ce.Role)
	}
}

func TestPhaseMutualAuth_ExternalAuthFails(t *testing.T) {
	rndEGK := bytes.Repeat([]byte{0xE1}, 8)
	smcbSig := bytes.Repeat([]byte{0xC0}, 64)
	egk := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 22 81 A4"), resp: mustHex("9000")},
		{match: mustHex("00 84 00 00 08"), resp: append(append([]byte{}, rndEGK...), 0x90, 0x00)},
		{match: mustHex("00 82 00 00"), resp: mustHex("6300")}, // auth failed
	}}
	smcb := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 88 00 00"), resp: append(append([]byte{}, smcbSig...), 0x90, 0x00)},
	}}
	h := newPhase4Handshake(t, egk, smcb)
	err := h.phaseMutualAuth()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %v", err)
	}
	if ce.Phase != PhaseMutualAuth || ce.Role != RoleEGK {
		t.Errorf("Phase=%v Role=%v", ce.Phase, ce.Role)
	}
}
