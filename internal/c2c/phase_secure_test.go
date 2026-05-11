package c2c

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // mirrors phase_secure.go derivation under test
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// newScaffoldHandshake builds a Handshake bypassing card I/O. The fakeCard
// type lives in discover_test.go; we just need *something* satisfying the
// Card interface so New() succeeds.
func newScaffoldHandshake(t *testing.T) *Handshake {
	t.Helper()
	c := &fakeCard{}
	h, err := New(Options{EGK: c, SMCB: c})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

// expectedKDF replicates the Algorithm-2 derivation used by phase_secure.go
// (SHA-1 over K||BE32(counter), truncated to 16 bytes). It exists so the
// test can detect regressions in deriveKeyAlg2, but it is NOT an
// independent spec-conformance check — both the test and the production
// code use the same SHA-1+truncation recipe. Once we obtain official
// gemSpec_Krypt test vectors, swap in a hard-coded expected key value.
func expectedKDF(nonceHost, nonceCard []byte, counter uint32) []byte {
	buf := make([]byte, 0, len(nonceHost)+len(nonceCard)+4)
	buf = append(buf, nonceHost...)
	buf = append(buf, nonceCard...)
	ctr := make([]byte, 4)
	binary.BigEndian.PutUint32(ctr, counter)
	buf = append(buf, ctr...)
	sum := sha1.Sum(buf) //nolint:gosec
	return sum[:16]
}

func TestPhaseOpenSecure_HappyPath_AES128(t *testing.T) {
	h := newScaffoldHandshake(t)

	// Fixed nonces — deterministic regression anchor.
	nonceHost := bytes.Repeat([]byte{0x11}, 16)
	nonceCard := bytes.Repeat([]byte{0x22}, 16)
	h.state.nonceHost = nonceHost
	h.state.nonceCard = nonceCard
	h.state.negotiatedAlg = "AES-128"

	if err := h.phaseOpenSecureChannel(); err != nil {
		t.Fatalf("phaseOpenSecureChannel: %v", err)
	}

	sess := h.Session()
	if sess == nil {
		t.Fatal("Session() returned nil after successful phase 5")
	}
	if got := len(sess.KEnc); got != 16 {
		t.Errorf("KEnc length = %d, want 16", got)
	}
	if got := len(sess.KMac); got != 16 {
		t.Errorf("KMac length = %d, want 16", got)
	}
	if got := len(sess.SSC); got != 16 {
		t.Fatalf("SSC length = %d, want 16", got)
	}
	zeros := make([]byte, 16)
	if !bytes.Equal(sess.SSC, zeros) {
		t.Errorf("SSC = % X, want all zeros", sess.SSC)
	}

	wantEnc := expectedKDF(nonceHost, nonceCard, 1)
	wantMac := expectedKDF(nonceHost, nonceCard, 2)
	if !bytes.Equal(sess.KEnc, wantEnc) {
		t.Errorf("KEnc mismatch:\n  got  % X\n  want % X", sess.KEnc, wantEnc)
	}
	if !bytes.Equal(sess.KMac, wantMac) {
		t.Errorf("KMac mismatch:\n  got  % X\n  want % X", sess.KMac, wantMac)
	}

	// KEnc and KMac must differ (different counter values).
	if bytes.Equal(sess.KEnc, sess.KMac) {
		t.Error("KEnc == KMac; derivation counter is not being applied")
	}
}

func TestPhaseOpenSecure_NonceHostNil(t *testing.T) {
	h := newScaffoldHandshake(t)
	h.state.nonceCard = bytes.Repeat([]byte{0x22}, 16)
	h.state.negotiatedAlg = "AES-128"

	err := h.phaseOpenSecureChannel()
	if err == nil {
		t.Fatal("expected precondition error for nil nonceHost")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T: %v", err, err)
	}
	if ce.Phase != PhaseOpenSecure {
		t.Errorf("Phase = %v, want PhaseOpenSecure", ce.Phase)
	}
	if !strings.Contains(ce.Msg, "nonceHost") {
		t.Errorf("Msg = %q, want it to mention nonceHost", ce.Msg)
	}
	if h.Session() != nil {
		t.Error("Session() must remain nil after a precondition failure")
	}
}

func TestPhaseOpenSecure_NonceCardNil(t *testing.T) {
	h := newScaffoldHandshake(t)
	h.state.nonceHost = bytes.Repeat([]byte{0x11}, 16)
	h.state.negotiatedAlg = "AES-128"

	err := h.phaseOpenSecureChannel()
	if err == nil {
		t.Fatal("expected precondition error for nil nonceCard")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T: %v", err, err)
	}
	if ce.Phase != PhaseOpenSecure {
		t.Errorf("Phase = %v, want PhaseOpenSecure", ce.Phase)
	}
	if !strings.Contains(ce.Msg, "nonceCard") {
		t.Errorf("Msg = %q, want it to mention nonceCard", ce.Msg)
	}
}

func TestPhaseOpenSecure_NegotiatedAlgEmpty(t *testing.T) {
	h := newScaffoldHandshake(t)
	h.state.nonceHost = bytes.Repeat([]byte{0x11}, 16)
	h.state.nonceCard = bytes.Repeat([]byte{0x22}, 16)
	// negotiatedAlg deliberately left empty.

	err := h.phaseOpenSecureChannel()
	if err == nil {
		t.Fatal("expected precondition error for empty negotiatedAlg")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T: %v", err, err)
	}
	if !strings.Contains(ce.Msg, "negotiatedAlg") {
		t.Errorf("Msg = %q, want it to mention negotiatedAlg", ce.Msg)
	}
}

func TestPhaseOpenSecure_UnsupportedAlgorithm(t *testing.T) {
	h := newScaffoldHandshake(t)
	h.state.nonceHost = bytes.Repeat([]byte{0x11}, 16)
	h.state.nonceCard = bytes.Repeat([]byte{0x22}, 16)
	h.state.negotiatedAlg = "AES-256" // valid name but not implemented yet

	err := h.phaseOpenSecureChannel()
	if err == nil {
		t.Fatal("expected error for unsupported negotiated algorithm")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T: %v", err, err)
	}
	if ce.Phase != PhaseOpenSecure {
		t.Errorf("Phase = %v, want PhaseOpenSecure", ce.Phase)
	}
	wantSub := "negotiated algorithm not supported: AES-256"
	if !strings.Contains(ce.Msg, wantSub) {
		t.Errorf("Msg = %q, want it to contain %q", ce.Msg, wantSub)
	}
	if h.Session() != nil {
		t.Error("Session() must remain nil after unsupported-algorithm error")
	}
}

func TestPhaseOpenSecure_SessionPopulatedViaPublicEntry(t *testing.T) {
	h := newScaffoldHandshake(t)
	h.state.nonceHost = bytes.Repeat([]byte{0xA5}, 16)
	h.state.nonceCard = bytes.Repeat([]byte{0x5A}, 16)
	h.state.negotiatedAlg = "AES-128"

	if err := h.OpenSecureChannel(); err != nil {
		t.Fatalf("OpenSecureChannel: %v", err)
	}
	if h.Session() == nil {
		t.Fatal("Session() is nil after OpenSecureChannel success")
	}
	if h.LastPhase() != PhaseOpenSecure {
		t.Errorf("LastPhase() = %v, want PhaseOpenSecure", h.LastPhase())
	}
	if h.LastError() != nil {
		t.Errorf("LastError() = %v, want nil", h.LastError())
	}
}

// TestPhaseOpenSecure_WrapRoundTrip exercises the integration with
// internal/c2c/sm: after deriving keys we should be able to Wrap a plain
// APDU and observe the gemSpec SM shape — protected CLA (high nibble),
// '87' cryptogram DO when data is present, and '8E' MAC trailer.
func TestPhaseOpenSecure_WrapRoundTrip(t *testing.T) {
	h := newScaffoldHandshake(t)
	h.state.nonceHost = bytes.Repeat([]byte{0x01}, 16)
	h.state.nonceCard = bytes.Repeat([]byte{0x02}, 16)
	h.state.negotiatedAlg = "AES-128"
	if err := h.phaseOpenSecureChannel(); err != nil {
		t.Fatalf("phaseOpenSecureChannel: %v", err)
	}

	// VERIFY PIN-style APDU: CLA 0x00 INS 0x20 P1 0x00 P2 0x82, data = "1234".
	wrapped, err := h.Session().Wrap(0x00, 0x20, 0x00, 0x82, []byte("1234"), -1)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if len(wrapped) < 5 {
		t.Fatalf("wrapped APDU too short: % X", wrapped)
	}

	// SM bits 0x0C must be set on the wrapped CLA.
	if wrapped[0]&0x0C != 0x0C {
		t.Errorf("wrapped CLA = 0x%02X, expected SM bits 0x0C set", wrapped[0])
	}
	// '87' cryptogram DO must appear (we had cmdData != nil).
	if !bytes.Contains(wrapped, []byte{0x87}) {
		t.Errorf("wrapped APDU missing 0x87 cryptogram DO: % X", wrapped)
	}
	// '8E' MAC DO must appear with length 0x08 (truncated CMAC).
	if !bytes.Contains(wrapped, []byte{0x8E, 0x08}) {
		t.Errorf("wrapped APDU missing 0x8E 08 MAC DO: % X", wrapped)
	}
}
