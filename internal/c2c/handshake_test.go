package c2c

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/christhomas/card-reader/internal/c2c/cvcert"
	"github.com/christhomas/card-reader/internal/c2c/keys"
)

func TestNew_RequiresBothCards(t *testing.T) {
	c := &fakeCard{}
	if _, err := New(Options{SMCB: c}); err == nil {
		t.Error("missing EGK: expected error, got nil")
	}
	if _, err := New(Options{EGK: c}); err == nil {
		t.Error("missing SMCB: expected error, got nil")
	}
	if _, err := New(Options{EGK: c, SMCB: c}); err != nil {
		t.Errorf("both set: expected nil err, got %v", err)
	}
}

func TestNew_DefaultsNowToCurrentTime(t *testing.T) {
	c := &fakeCard{}
	before := time.Now()
	h, err := New(Options{EGK: c, SMCB: c})
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	if h.now.Before(before) || h.now.After(after) {
		t.Errorf("now=%v outside [%v, %v]", h.now, before, after)
	}
}

func TestDiscoverPeerCerts_NoCertsErrorsWithSubcauses(t *testing.T) {
	// SMC-B returns 6A82 to every SELECT EF — no CV-certs anywhere.
	smcb := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 A4 00 0C 02 3F 00"), resp: mustHex("9000")},
		{match: mustHex("00 A4 04 0C"), resp: mustHex("9000")}, // any AID select succeeds
		{match: mustHex("00 A4 02 0C 02"), resp: mustHex("6A82")},
	}}
	h, err := New(Options{EGK: smcb, SMCB: smcb})
	if err != nil {
		t.Fatal(err)
	}
	err = h.DiscoverPeerCerts()
	if err == nil {
		t.Fatal("expected error when no CV-certs present")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T", err)
	}
	if ce.Phase != PhaseDiscover || ce.Role != RoleSMCB {
		t.Errorf("Phase=%v Role=%v", ce.Phase, ce.Role)
	}
}

func TestValidatePeerChain_RequiresDiscoverFirst(t *testing.T) {
	c := &fakeCard{}
	h, _ := New(Options{EGK: c, SMCB: c, Roots: keys.TestRoots()})
	if err := h.ValidatePeerChain(); err == nil {
		t.Error("expected error when Discover hasn't run")
	}
}

func TestValidatePeerChain_RequiresRoots(t *testing.T) {
	c := &fakeCard{}
	h, _ := New(Options{EGK: c, SMCB: c})
	// Inject a fake discovered chain so we can isolate the "no roots" path.
	h.smcbChain = []DiscoveredCert{{Cert: &cvcert.Cert{CAR: "X", CHR: "Y"}}}
	err := h.ValidatePeerChain()
	if err == nil {
		t.Fatal("expected error when Roots is empty")
	}
	if !strings.Contains(err.Error(), "no CVC-Root trust anchors") {
		t.Errorf("err = %v", err)
	}
}

func TestPresentToVerifier_RequiresDiscoverFirst(t *testing.T) {
	// Without PhaseDiscover running, smcbChain is empty and phase 3
	// must refuse rather than issue a no-op stream of APDUs.
	c := &fakeCard{}
	h, _ := New(Options{EGK: c, SMCB: c})
	err := h.PresentToVerifier()
	if err == nil {
		t.Fatal("expected precondition error")
	}
	if !strings.Contains(err.Error(), "chain is empty") {
		t.Errorf("err = %v", err)
	}
}

func TestMutualAuthenticate_IsScaffolded(t *testing.T) {
	c := &fakeCard{}
	h, _ := New(Options{EGK: c, SMCB: c})
	if err := h.MutualAuthenticate(); err == nil {
		t.Error("expected scaffold error")
	}
}

func TestOpenSecureChannel_IsScaffolded(t *testing.T) {
	c := &fakeCard{}
	h, _ := New(Options{EGK: c, SMCB: c})
	if err := h.OpenSecureChannel(); err == nil {
		t.Error("expected scaffold error")
	}
}

func TestRun_StopsAtFirstError(t *testing.T) {
	// SMC-B with no certs → DiscoverPeerCerts fails. Run() should stop there
	// and not call later phases.
	smcb := &fakeCard{scripts: []scriptEntry{
		{match: mustHex("00 A4 00 0C 02 3F 00"), resp: mustHex("9000")},
		{match: mustHex("00 A4 04 0C"), resp: mustHex("9000")},
		{match: mustHex("00 A4 02 0C 02"), resp: mustHex("6A82")},
	}}
	h, _ := New(Options{EGK: smcb, SMCB: smcb, Roots: keys.TestRoots()})
	err := h.Run()
	if err == nil {
		t.Fatal("expected Run to return error")
	}
	var ce *Error
	if !errors.As(err, &ce) || ce.Phase != PhaseDiscover {
		t.Errorf("expected PhaseDiscover error, got %v", err)
	}
}

func TestError_FormattedString(t *testing.T) {
	e := &Error{
		Phase: PhaseValidateChain,
		Role:  RoleSMCB,
		Msg:   "x",
		Cause: errors.New("cause-y"),
	}
	s := e.Error()
	for _, want := range []string{"c2c:", "validate-peer-chain", "SMC-B", "x", "cause-y"} {
		if !strings.Contains(s, want) {
			t.Errorf("Error() missing %q in %q", want, s)
		}
	}
}

func TestPhase_String(t *testing.T) {
	for _, tc := range []struct {
		p    Phase
		want string
	}{
		{PhaseDiscover, "discover-peer-certs"},
		{PhaseValidateChain, "validate-peer-chain"},
		{PhasePresentToVerifier, "present-to-verifier"},
		{PhaseMutualAuth, "mutual-authenticate"},
		{PhaseOpenSecure, "open-secure-channel"},
		{Phase(99), "phase-99"},
	} {
		if got := tc.p.String(); got != tc.want {
			t.Errorf("Phase(%d).String() = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestRole_String(t *testing.T) {
	for _, tc := range []struct {
		r    Role
		want string
	}{
		{RoleEGK, "eGK"},
		{RoleSMCB, "SMC-B"},
		{RoleHost, "host"},
		{Role(99), "unknown"},
	} {
		if got := tc.r.String(); got != tc.want {
			t.Errorf("Role(%d).String() = %q, want %q", tc.r, got, tc.want)
		}
	}
}

func TestSMCBChain_ReturnsInternalSlice(t *testing.T) {
	c := &fakeCard{}
	h, _ := New(Options{EGK: c, SMCB: c})
	if got := h.SMCBChain(); got != nil {
		t.Errorf("expected nil before discovery, got %v", got)
	}
	want := []DiscoveredCert{{Cert: &cvcert.Cert{CAR: "A", CHR: "B"}}}
	h.smcbChain = want
	got := h.SMCBChain()
	if len(got) != 1 || got[0].Cert.CHR != "B" {
		t.Errorf("SMCBChain() = %+v, want %+v", got, want)
	}
}

func TestMatchedRoot_ReturnsInternalRoot(t *testing.T) {
	c := &fakeCard{}
	h, _ := New(Options{EGK: c, SMCB: c})
	if got := h.MatchedRoot(); got != nil {
		t.Errorf("expected nil before validation, got %+v", got)
	}
	r := &keys.Root{Name: "X"}
	h.matchedRoot = r
	if got := h.MatchedRoot(); got != r {
		t.Errorf("MatchedRoot() = %p, want %p", got, r)
	}
}

func TestValidatePeerChain_ChainRejected(t *testing.T) {
	// Provide roots but a chain with empty Body — keys.VerifyChain
	// rejects on "missing Body/Signature" and the error must be wrapped
	// in *c2c.Error with PhaseValidateChain + RoleSMCB.
	c := &fakeCard{}
	h, _ := New(Options{EGK: c, SMCB: c, Roots: keys.TestRoots()})
	h.smcbChain = []DiscoveredCert{{Cert: &cvcert.Cert{CAR: "X", CHR: "Y"}}}
	err := h.ValidatePeerChain()
	if err == nil {
		t.Fatal("expected error for chain with empty Body")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T", err)
	}
	if ce.Phase != PhaseValidateChain || ce.Role != RoleSMCB {
		t.Errorf("Phase=%v Role=%v", ce.Phase, ce.Role)
	}
	if ce.Cause == nil {
		t.Error("expected wrapped Cause from keys.VerifyChain")
	}
	if !strings.Contains(err.Error(), "rejected by all configured roots") {
		t.Errorf("err = %v", err)
	}
}

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := &Error{Phase: PhaseValidateChain, Cause: cause}
	if got := errors.Unwrap(e); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
	// Nil-cause: Unwrap returns nil.
	e2 := &Error{Phase: PhaseDiscover}
	if got := errors.Unwrap(e2); got != nil {
		t.Errorf("Unwrap() with nil Cause = %v, want nil", got)
	}
}

func TestError_StringWithoutCauseOrSub(t *testing.T) {
	// Covers the branch where Cause == nil and Sub == nil — only Phase
	// and Msg appear. Also covers Role == 0 (no role segment).
	e := &Error{Phase: PhaseDiscover, Msg: "x"}
	s := e.Error()
	if !strings.Contains(s, "c2c:") || !strings.Contains(s, "discover-peer-certs") || !strings.Contains(s, "x") {
		t.Errorf("Error() = %q", s)
	}
	if strings.Contains(s, "cause:") || strings.Contains(s, "sub:") || strings.Contains(s, "role=") {
		t.Errorf("unexpected segment in %q", s)
	}
}

func TestValidatePeerChain_SuccessPopulatesMatchedRoot(t *testing.T) {
	// Build a single-cert chain signed by a synthetic RSA root, register
	// the root in Options.Roots, and assert ValidatePeerChain accepts it
	// and records MatchedRoot. This covers the success branch (matchedRoot
	// assignment + return nil) that the error tests can't reach.
	rootPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey root: %v", err)
	}
	leafPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey leaf: %v", err)
	}
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	body := []byte("cvcert-body|car=TEST_ROOT|chr=TEST_LEAF")
	h := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, rootPriv, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}
	leaf := &cvcert.Cert{
		CAR:       "TEST_ROOT",
		CHR:       "TEST_LEAF",
		NotBefore: now.Add(-24 * time.Hour),
		NotAfter:  now.Add(24 * time.Hour),
		KeyAlg:    cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: leafPriv.N, E: big.NewInt(int64(leafPriv.E))},
		Body:      body,
		Signature: sig,
	}
	root := keys.Root{
		Name:      "TEST_ROOT",
		KeyAlg:    cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: rootPriv.N, E: big.NewInt(int64(rootPriv.E))},
	}

	c := &fakeCard{}
	hs, _ := New(Options{EGK: c, SMCB: c, Roots: []keys.Root{root}, Now: now})
	hs.smcbChain = []DiscoveredCert{{Cert: leaf}}
	if err := hs.ValidatePeerChain(); err != nil {
		t.Fatalf("ValidatePeerChain: %v", err)
	}
	mr := hs.MatchedRoot()
	if mr == nil || mr.Name != "TEST_ROOT" {
		t.Errorf("MatchedRoot = %+v, want TEST_ROOT", mr)
	}
}

func TestError_StringWithSub(t *testing.T) {
	// Covers the Sub != nil branch (independent of Cause).
	e := &Error{Phase: PhaseDiscover, Sub: errors.New("sub-x")}
	s := e.Error()
	if !strings.Contains(s, "sub: sub-x") {
		t.Errorf("Error() = %q, missing sub", s)
	}
}

func TestJoinErrors(t *testing.T) {
	if got := joinErrors(nil); got != nil {
		t.Errorf("joinErrors(nil) = %v, want nil", got)
	}
	if got := joinErrors([]error{}); got != nil {
		t.Errorf("joinErrors(empty) = %v, want nil", got)
	}
	a := errors.New("a")
	b := errors.New("b")
	got := joinErrors([]error{a, b})
	if got == nil || !errors.Is(got, a) || !errors.Is(got, b) {
		t.Errorf("joinErrors([a,b]) = %v, want joined", got)
	}
}
