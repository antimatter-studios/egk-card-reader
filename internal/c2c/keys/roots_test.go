package keys

import (
	"strings"
	"testing"

	"github.com/christhomas/card-reader/internal/c2c/cvcert"
)

// Embedded root expectations. Cross-check against sources.md.
var expectedProdRoots = map[string]string{
	"gematik.RCA2": "848fda162c607b492c62f625840e6451285c40c7334ec8dd659d093236ebc9ec",
	"gematik.RCA6": "7c250199c7d87058a3a8f84f2a3c7727a27511670dac596535273af0452d84f3",
	"gematik.RCA9": "b7eee57557c31d43263d5e6cfe98185acf2b7d338c2261a054368d5dd5432442",
}

var expectedTestRoots = map[string]string{
	"gematik.RCA2.TEST-ONLY": "074609b1d76a19286efcb90634a0d6aea36826ee1ffc52c696235b7f4a87872d",
	"gematik.RCA6.TEST-ONLY": "3cff0528cf0ff06e5a99f157afad505bca9dfa012861a471f71ca98cea5721ed",
	"gematik.RCA9.TEST-ONLY": "75bb81da87f1841dc667868149df04e469c2006e1da0d27eb1c46d0a234c55bd",
}

func TestProductionRoots_Names(t *testing.T) {
	got := ProductionRoots()
	if len(got) != len(expectedProdRoots) {
		t.Fatalf("ProductionRoots returned %d entries, want %d", len(got), len(expectedProdRoots))
	}
	for _, r := range got {
		fp, ok := expectedProdRoots[r.Name]
		if !ok {
			t.Errorf("unexpected production root %q", r.Name)
			continue
		}
		if r.Fingerprint != fp {
			t.Errorf("root %q: fingerprint = %q, want %q", r.Name, r.Fingerprint, fp)
		}
		if r.KeyAlg != cvcert.AlgRSA2048 {
			t.Errorf("root %q: KeyAlg = %v, want AlgRSA2048", r.Name, r.KeyAlg)
		}
		pk, ok := r.PublicKey.(*cvcert.RSAPublicKey)
		if !ok {
			t.Errorf("root %q: PublicKey is %T, want *cvcert.RSAPublicKey", r.Name, r.PublicKey)
			continue
		}
		if pk.N.BitLen() < 2040 || pk.N.BitLen() > 2048 {
			t.Errorf("root %q: modulus bit length = %d, want ~2048", r.Name, pk.N.BitLen())
		}
		if pk.E.Int64() != 65537 {
			t.Errorf("root %q: exponent = %d, want 65537", r.Name, pk.E.Int64())
		}
		if !strings.HasPrefix(r.Source, "https://download.tsl.ti-dienste.de/") {
			t.Errorf("root %q: source = %q, want https://download.tsl.ti-dienste.de/...", r.Name, r.Source)
		}
	}
}

func TestTestRoots_Names(t *testing.T) {
	got := TestRoots()
	if len(got) != len(expectedTestRoots) {
		t.Fatalf("TestRoots returned %d entries, want %d", len(got), len(expectedTestRoots))
	}
	for _, r := range got {
		fp, ok := expectedTestRoots[r.Name]
		if !ok {
			t.Errorf("unexpected test root %q", r.Name)
			continue
		}
		if r.Fingerprint != fp {
			t.Errorf("root %q: fingerprint = %q, want %q", r.Name, r.Fingerprint, fp)
		}
		if !strings.Contains(r.SubjectDN, "TEST-ONLY") {
			t.Errorf("root %q: SubjectDN does not contain TEST-ONLY: %q", r.Name, r.SubjectDN)
		}
		if !strings.Contains(r.SubjectDN, "NOT-VALID") {
			t.Errorf("root %q: SubjectDN does not contain NOT-VALID: %q", r.Name, r.SubjectDN)
		}
		if !strings.HasPrefix(r.Source, "https://download-test.tsl.ti-dienste.de/") {
			t.Errorf("root %q: source = %q, want https://download-test.tsl.ti-dienste.de/...", r.Name, r.Source)
		}
	}
}

func TestAllRoots_Combines(t *testing.T) {
	all := AllRoots()
	want := len(ProductionRoots()) + len(TestRoots())
	if len(all) != want {
		t.Fatalf("AllRoots returned %d entries, want %d", len(all), want)
	}
}

// Sanity: roots have non-zero validity.
func TestRoots_ValidityRangesNonEmpty(t *testing.T) {
	for _, r := range AllRoots() {
		if r.NotBefore.IsZero() || r.NotAfter.IsZero() {
			t.Errorf("root %q has zero validity bounds", r.Name)
		}
		if !r.NotBefore.Before(r.NotAfter) {
			t.Errorf("root %q has NotBefore >= NotAfter (%s >= %s)", r.Name, r.NotBefore, r.NotAfter)
		}
	}
}
