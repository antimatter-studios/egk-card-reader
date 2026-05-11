package c2c

import (
	"errors"
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
