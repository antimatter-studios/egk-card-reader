package brainpool

import (
	"math/big"
	"testing"
)

// TestAddPPEqualsDouble — Add(P, P) must equal Double(P).
func TestAddPPEqualsDouble(t *testing.T) {
	c := P256r1()
	ax, ay := c.Add(c.Gx, c.Gy, c.Gx, c.Gy)
	dx, dy := c.Double(c.Gx, c.Gy)
	if ax.Cmp(dx) != 0 || ay.Cmp(dy) != 0 {
		t.Fatalf("Add(G,G) != Double(G):\n add  =(%x,%x)\n dbl  =(%x,%x)", ax, ay, dx, dy)
	}
}

// TestAddInverseGivesInfinity — P + (-P) must be the point at infinity.
func TestAddInverseGivesInfinity(t *testing.T) {
	c := P256r1()
	negY := new(big.Int).Sub(c.P, c.Gy)
	x, y := c.Add(c.Gx, c.Gy, c.Gx, negY)
	if !isInfinity(x, y) {
		t.Fatalf("G + (-G) != O, got (%x, %x)", x, y)
	}
}

// TestScalarMult2GMatchesGPlusG — 2*G via ScalarMult must match G+G.
func TestScalarMult2GMatchesGPlusG(t *testing.T) {
	c := P256r1()
	addX, addY := c.Add(c.Gx, c.Gy, c.Gx, c.Gy)
	smX, smY := c.ScalarBaseMult([]byte{2})
	if addX.Cmp(smX) != 0 || addY.Cmp(smY) != 0 {
		t.Fatalf("2*G via ScalarMult differs from G+G")
	}
}

// TestScalarMultAssociative — 3*G computed two ways must agree.
func TestScalarMultAssociative(t *testing.T) {
	c := P256r1()
	// (2G) + G
	twoX, twoY := c.ScalarBaseMult([]byte{2})
	wayA_x, wayA_y := c.Add(twoX, twoY, c.Gx, c.Gy)
	// 3*G
	wayB_x, wayB_y := c.ScalarBaseMult([]byte{3})
	if wayA_x.Cmp(wayB_x) != 0 || wayA_y.Cmp(wayB_y) != 0 {
		t.Fatalf("3*G inconsistent: (2G)+G=(%x,%x) vs 3G=(%x,%x)",
			wayA_x, wayA_y, wayB_x, wayB_y)
	}
}

// TestScalarMultProducesOnCurvePoints — every scalar multiple must land
// on the curve.
func TestScalarMultProducesOnCurvePoints(t *testing.T) {
	c := P256r1()
	for k := 1; k < 20; k++ {
		x, y := c.ScalarBaseMult([]byte{byte(k)})
		if !c.IsOnCurve(x, y) {
			t.Fatalf("%d*G not on curve: (%x, %x)", k, x, y)
		}
	}
}

// TestAddInfinityIdentity — O + P == P and P + O == P.
func TestAddInfinityIdentity(t *testing.T) {
	c := P256r1()
	x1, y1 := c.Add(nil, nil, c.Gx, c.Gy)
	if x1.Cmp(c.Gx) != 0 || y1.Cmp(c.Gy) != 0 {
		t.Fatalf("O+G != G")
	}
	x2, y2 := c.Add(c.Gx, c.Gy, nil, nil)
	if x2.Cmp(c.Gx) != 0 || y2.Cmp(c.Gy) != 0 {
		t.Fatalf("G+O != G")
	}
	x3, y3 := c.Add(nil, nil, nil, nil)
	if !isInfinity(x3, y3) {
		t.Fatalf("O+O != O")
	}
}

// TestDoubleOfInfinity — 2*O == O.
func TestDoubleOfInfinity(t *testing.T) {
	c := P256r1()
	x, y := c.Double(nil, nil)
	if !isInfinity(x, y) {
		t.Fatalf("2*O != O")
	}
}

// TestScalarMultByZero — 0*G == O.
func TestScalarMultByZero(t *testing.T) {
	c := P256r1()
	x, y := c.ScalarBaseMult([]byte{0})
	if !isInfinity(x, y) {
		t.Fatalf("0*G != O: (%x, %x)", x, y)
	}
	x, y = c.ScalarBaseMult(nil)
	if !isInfinity(x, y) {
		t.Fatalf("nil scalar -> not O")
	}
}

// TestScalarMultLargeScalar — (N-1)*G + G == O (since N*G == O).
func TestScalarMultLargeScalar(t *testing.T) {
	c := P256r1()
	nMinus1 := new(big.Int).Sub(c.N, big.NewInt(1))
	x, y := c.ScalarBaseMult(nMinus1.Bytes())
	sumX, sumY := c.Add(x, y, c.Gx, c.Gy)
	if !isInfinity(sumX, sumY) {
		t.Fatalf("(N-1)G + G != O")
	}
}
