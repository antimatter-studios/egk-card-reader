package brainpool

import (
	"math/big"
	"testing"
)

// TestCurveParamsConsistency checks the documented invariants of each
// curve: P and N are prime-sized, G is on the curve, and N*G is the
// point at infinity.
func TestCurveParamsConsistency(t *testing.T) {
	for _, c := range []*Curve{P256r1(), P384r1(), P512r1()} {
		t.Run(c.Name, func(t *testing.T) {
			if c.P.BitLen() != c.BitSize {
				t.Errorf("P.BitLen=%d want %d", c.P.BitLen(), c.BitSize)
			}
			if c.N.BitLen() != c.BitSize {
				t.Errorf("N.BitLen=%d want %d", c.N.BitLen(), c.BitSize)
			}
			if c.H != 1 {
				t.Errorf("cofactor H=%d, all Brainpool r1 curves have H=1", c.H)
			}
			if !c.IsOnCurve(c.Gx, c.Gy) {
				t.Fatalf("generator G is not on curve %s", c.Name)
			}
			// N * G must be the point at infinity.
			x, y := c.ScalarBaseMult(c.N.Bytes())
			if !isInfinity(x, y) {
				t.Fatalf("N*G is not infinity for %s: got (%x, %x)", c.Name, x, y)
			}
		})
	}
}

// TestGeneratorOrder1 — 1*G == G.
func TestGeneratorOrder1(t *testing.T) {
	for _, c := range []*Curve{P256r1(), P384r1(), P512r1()} {
		t.Run(c.Name, func(t *testing.T) {
			x, y := c.ScalarBaseMult([]byte{1})
			if x.Cmp(c.Gx) != 0 || y.Cmp(c.Gy) != 0 {
				t.Fatalf("1*G != G for %s", c.Name)
			}
		})
	}
}

// TestSingletonsReturnSameInstance — singletons must be cached.
func TestSingletonsReturnSameInstance(t *testing.T) {
	if P256r1() != P256r1() {
		t.Error("P256r1 singleton not cached")
	}
	if P384r1() != P384r1() {
		t.Error("P384r1 singleton not cached")
	}
	if P512r1() != P512r1() {
		t.Error("P512r1 singleton not cached")
	}
}

// TestIsOnCurveRejectsOffCurve — perturbing y by 1 must give a non-point.
func TestIsOnCurveRejectsOffCurve(t *testing.T) {
	c := P256r1()
	badY := new(big.Int).Add(c.Gy, big.NewInt(1))
	badY.Mod(badY, c.P)
	if c.IsOnCurve(c.Gx, badY) {
		t.Fatal("(Gx, Gy+1) reported as on curve")
	}
	if c.IsOnCurve(nil, nil) {
		t.Fatal("(nil, nil) reported as on curve")
	}
	if c.IsOnCurve(big.NewInt(0), big.NewInt(0)) {
		t.Fatal("(0, 0) reported as on curve")
	}
}
