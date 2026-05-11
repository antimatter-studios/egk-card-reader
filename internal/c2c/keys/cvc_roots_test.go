package keys

import (
	"strings"
	"testing"

	"github.com/christhomas/card-reader/internal/c2c/brainpool"
	"github.com/christhomas/card-reader/internal/c2c/cvcert"
)

func TestProductionCVCRoots_NonEmpty(t *testing.T) {
	r := ProductionCVCRoots()
	if len(r) == 0 {
		t.Fatal("ProductionCVCRoots returned empty slice")
	}
	for _, root := range r {
		if !strings.HasPrefix(root.Name, "gematik.CVC-Root.PU") {
			t.Errorf("PU root name doesn't have PU prefix: %s", root.Name)
		}
	}
}

func TestTestCVCRoots_NonEmpty(t *testing.T) {
	r := TestCVCRoots()
	if len(r) == 0 {
		t.Fatal("TestCVCRoots returned empty slice")
	}
	for _, root := range r {
		if !strings.HasPrefix(root.Name, "gematik.CVC-Root.RU-TU") {
			t.Errorf("TEST root name doesn't have RU-TU prefix: %s", root.Name)
		}
	}
}

func TestCVCRoots_AlgIsBrainpoolP256(t *testing.T) {
	for _, root := range AllCVCRoots() {
		if root.KeyAlg != cvcert.AlgBrainpoolP256r1 {
			t.Errorf("%s: KeyAlg = %v, want AlgBrainpoolP256r1", root.Name, root.KeyAlg)
		}
		pk, ok := root.PublicKey.(*cvcert.ECCPublicKey)
		if !ok {
			t.Errorf("%s: PublicKey type %T, want *cvcert.ECCPublicKey", root.Name, root.PublicKey)
			continue
		}
		if pk.X == nil || pk.Y == nil {
			t.Errorf("%s: nil coordinate", root.Name)
		}
	}
}

func TestCVCRoots_PointsOnCurve(t *testing.T) {
	curve := brainpool.P256r1()
	for _, root := range AllCVCRoots() {
		pk, ok := root.PublicKey.(*cvcert.ECCPublicKey)
		if !ok {
			t.Errorf("%s: not ECC", root.Name)
			continue
		}
		if !curve.IsOnCurve(pk.X, pk.Y) {
			t.Errorf("%s: public key (X=%X, Y=%X) is NOT on Brainpool P-256r1 — research-log coordinate transcription is wrong",
				root.Name, pk.X.Bytes(), pk.Y.Bytes())
		}
	}
}

func TestCVCRoots_FingerprintsAreSHA256Hex(t *testing.T) {
	for _, root := range AllCVCRoots() {
		if root.Fingerprint == "" {
			continue // some inactive roots intentionally left blank
		}
		if len(root.Fingerprint) != 64 {
			t.Errorf("%s: fingerprint len %d, want 64 (sha256 hex)", root.Name, len(root.Fingerprint))
		}
		for _, c := range root.Fingerprint {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("%s: fingerprint contains non-hex char %q", root.Name, c)
				break
			}
		}
	}
}

func TestCVCRoots_AktivRootsHaveSources(t *testing.T) {
	// The current active production + test roots MUST have a source URL
	// and a fingerprint so we can re-fetch and re-verify if a key looks
	// wrong in the field.
	want := []string{
		"gematik.CVC-Root.PU.7.DEZGW870226",
		"gematik.CVC-Root.RU-TU.8.DEGXX890225",
	}
	byName := make(map[string]Root)
	for _, r := range AllCVCRoots() {
		byName[r.Name] = r
	}
	for _, n := range want {
		r, ok := byName[n]
		if !ok {
			t.Errorf("missing expected active root %q", n)
			continue
		}
		if r.Source == "" {
			t.Errorf("%s: empty Source", n)
		}
		if r.Fingerprint == "" {
			t.Errorf("%s: empty Fingerprint", n)
		}
	}
}
