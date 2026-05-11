package brainpool

import "math/big"

// VerifyECDSA verifies an ECDSA signature (r, s) against public key
// Q = (qx, qy) on the curve c, over a pre-hashed message digest `hashed`.
// Returns true iff the signature is valid. Follows SEC 1 v2.0 §4.1.4:
//
//  1. Reject if r or s is not in [1, n-1].
//  2. Reject if Q is the point at infinity or not on c.
//  3. e := leftmost ceil(log2(n)) bits of hashed (here: leftmost
//     BitSize bits; for Brainpool the order is exactly BitSize bits).
//  4. w := s^-1 mod n
//  5. u1 := e*w mod n, u2 := r*w mod n
//  6. (x1, y1) := u1*G + u2*Q ; if it's the point at infinity, reject.
//  7. Valid iff x1 mod n == r.
//
// Caller is responsible for choosing an appropriate hash function (e.g.
// SHA-256 for P256r1, SHA-384 for P384r1, SHA-512 for P512r1) and for
// passing the digest in big-endian byte order. SEC 1 truncates the hash
// to the bit-length of n.
func (c *Curve) VerifyECDSA(qx, qy *big.Int, hashed []byte, r, s *big.Int) bool {
	// Defensive nil checks.
	if qx == nil || qy == nil || r == nil || s == nil {
		return false
	}
	// (1) r, s in [1, n-1].
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return false
	}
	if r.Cmp(c.N) >= 0 || s.Cmp(c.N) >= 0 {
		return false
	}
	// (2) Q is on the curve and not at infinity.
	//     IsOnCurve already rejects (nil, nil) and out-of-range coords.
	if !c.IsOnCurve(qx, qy) {
		return false
	}
	//     Also check that n*Q == O. For Brainpool r1 curves the cofactor
	//     h = 1, so any on-curve non-infinity point already has order
	//     dividing n (prime). This check is therefore redundant but
	//     follows SEC 1's stricter form and costs little for verify.
	nqx, nqy := c.ScalarMult(qx, qy, c.N.Bytes())
	if !isInfinity(nqx, nqy) {
		return false
	}

	// (3) e := truncated digest.
	e := hashToInt(hashed, c)

	// (4) w := s^-1 mod n.
	w := new(big.Int).ModInverse(s, c.N)
	if w == nil {
		return false
	}

	// (5) u1, u2.
	u1 := new(big.Int).Mul(e, w)
	u1.Mod(u1, c.N)
	u2 := new(big.Int).Mul(r, w)
	u2.Mod(u2, c.N)

	// (6) point := u1*G + u2*Q.
	x1, y1 := c.ScalarBaseMult(u1.Bytes())
	x2, y2 := c.ScalarMult(qx, qy, u2.Bytes())
	rx, ry := c.Add(x1, y1, x2, y2)
	if isInfinity(rx, ry) {
		return false
	}
	_ = ry // y-coordinate not needed for verification

	// (7) rx mod n == r ?
	rx.Mod(rx, c.N)
	return rx.Cmp(r) == 0
}

// hashToInt converts a hash value to an integer per SEC 1 §4.1.3 step 5:
// take the leftmost N.BitLen() bits.
func hashToInt(hash []byte, c *Curve) *big.Int {
	orderBits := c.N.BitLen()
	orderBytes := (orderBits + 7) / 8
	if len(hash) > orderBytes {
		hash = hash[:orderBytes]
	}
	ret := new(big.Int).SetBytes(hash)
	excess := len(hash)*8 - orderBits
	if excess > 0 {
		ret.Rsh(ret, uint(excess))
	}
	return ret
}
