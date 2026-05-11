package brainpool

import "math/big"

// Affine point arithmetic on a short-Weierstrass curve y^2 = x^3 + Ax + B
// over GF(P). The point at infinity is represented as (nil, nil); the
// public methods accept and return that convention. Internally we use
// the standard textbook formulae (Hankerson, Menezes, Vanstone — "Guide
// to Elliptic Curve Cryptography" §3.1.2). No constant-time tricks: the
// only consumer here is signature verification, which operates on public
// inputs.

// isInfinity reports whether (x, y) encodes the point at infinity.
func isInfinity(x, y *big.Int) bool {
	return x == nil && y == nil
}

// mod returns z mod P in the canonical range [0, P).
func (c *Curve) mod(z *big.Int) *big.Int {
	r := new(big.Int).Mod(z, c.P)
	if r.Sign() < 0 {
		r.Add(r, c.P)
	}
	return r
}

// IsOnCurve reports whether the affine point (x, y) lies on c.
// The point at infinity returns false (callers that need to admit it
// should check explicitly).
func (c *Curve) IsOnCurve(x, y *big.Int) bool {
	if x == nil || y == nil {
		return false
	}
	// 0 <= x, y < P
	if x.Sign() < 0 || y.Sign() < 0 {
		return false
	}
	if x.Cmp(c.P) >= 0 || y.Cmp(c.P) >= 0 {
		return false
	}
	// y^2 mod P
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, c.P)
	// x^3 + A*x + B mod P
	rhs := new(big.Int).Mul(x, x)
	rhs.Mod(rhs, c.P)
	rhs.Mul(rhs, x)
	rhs.Mod(rhs, c.P)
	ax := new(big.Int).Mul(c.A, x)
	rhs.Add(rhs, ax)
	rhs.Add(rhs, c.B)
	rhs.Mod(rhs, c.P)
	return y2.Cmp(rhs) == 0
}

// Add returns (x1, y1) + (x2, y2) on c. The point at infinity is
// encoded as both coordinates nil.
func (c *Curve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	if isInfinity(x1, y1) {
		if isInfinity(x2, y2) {
			return nil, nil
		}
		return new(big.Int).Set(x2), new(big.Int).Set(y2)
	}
	if isInfinity(x2, y2) {
		return new(big.Int).Set(x1), new(big.Int).Set(y1)
	}
	// If x1 == x2 then either we double or we land at infinity.
	if x1.Cmp(x2) == 0 {
		// y1 + y2 == 0 (mod P)  =>  P + (-P) = O
		ysum := new(big.Int).Add(y1, y2)
		ysum.Mod(ysum, c.P)
		if ysum.Sign() == 0 {
			return nil, nil
		}
		// otherwise y1 == y2 — doubling
		return c.Double(x1, y1)
	}
	// Slope: lambda = (y2 - y1) / (x2 - x1) mod P
	num := new(big.Int).Sub(y2, y1)
	den := new(big.Int).Sub(x2, x1)
	den = c.mod(den)
	denInv := new(big.Int).ModInverse(den, c.P)
	if denInv == nil {
		// gcd(den, P) != 1 — should be impossible for a prime P with
		// x1 != x2 in [0, P). Return infinity defensively.
		return nil, nil
	}
	lambda := new(big.Int).Mul(num, denInv)
	lambda = c.mod(lambda)
	// x3 = lambda^2 - x1 - x2
	x3 := new(big.Int).Mul(lambda, lambda)
	x3.Sub(x3, x1)
	x3.Sub(x3, x2)
	x3 = c.mod(x3)
	// y3 = lambda * (x1 - x3) - y1
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, lambda)
	y3.Sub(y3, y1)
	y3 = c.mod(y3)
	return x3, y3
}

// Double returns 2 * (x, y) on c.
func (c *Curve) Double(x, y *big.Int) (*big.Int, *big.Int) {
	if isInfinity(x, y) {
		return nil, nil
	}
	if y.Sign() == 0 {
		// Tangent is vertical — 2P = O.
		return nil, nil
	}
	// Slope: lambda = (3*x^2 + A) / (2*y) mod P
	num := new(big.Int).Mul(x, x)
	num.Mul(num, big.NewInt(3))
	num.Add(num, c.A)
	den := new(big.Int).Lsh(y, 1) // 2y
	den = c.mod(den)
	denInv := new(big.Int).ModInverse(den, c.P)
	if denInv == nil {
		return nil, nil
	}
	lambda := new(big.Int).Mul(num, denInv)
	lambda = c.mod(lambda)
	// x3 = lambda^2 - 2x
	x3 := new(big.Int).Mul(lambda, lambda)
	x3.Sub(x3, x)
	x3.Sub(x3, x)
	x3 = c.mod(x3)
	// y3 = lambda * (x - x3) - y
	y3 := new(big.Int).Sub(x, x3)
	y3.Mul(y3, lambda)
	y3.Sub(y3, y)
	y3 = c.mod(y3)
	return x3, y3
}

// ScalarMult returns k * (x, y) on c, where k is a big-endian byte slice
// (same convention as crypto/elliptic). Uses left-to-right double-and-add.
// k is treated as an unsigned integer; it is NOT reduced modulo N, so a
// k > N will give k*P (which equals (k mod N)*P for points of order N).
func (c *Curve) ScalarMult(x, y *big.Int, k []byte) (*big.Int, *big.Int) {
	if isInfinity(x, y) {
		return nil, nil
	}
	scalar := new(big.Int).SetBytes(k)
	if scalar.Sign() == 0 {
		return nil, nil
	}
	var rx, ry *big.Int // start at infinity
	bx := new(big.Int).Set(x)
	by := new(big.Int).Set(y)
	nbits := scalar.BitLen()
	for i := nbits - 1; i >= 0; i-- {
		rx, ry = c.Double(rx, ry)
		if scalar.Bit(i) == 1 {
			rx, ry = c.Add(rx, ry, bx, by)
		}
	}
	return rx, ry
}

// ScalarBaseMult returns k * G on c.
func (c *Curve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) {
	return c.ScalarMult(c.Gx, c.Gy, k)
}
