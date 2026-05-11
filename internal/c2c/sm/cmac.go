package sm

import (
	"crypto/cipher"
	"errors"
)

// errShortKey is returned for an obviously-wrong key length.
var errShortKey = errors.New("sm: invalid AES key length (expected 16, 24, or 32 bytes)")

// cmacSubkeys derives the two subkeys K1, K2 used by CMAC per
// NIST SP 800-38B §6.1. For AES (128-bit block) the irreducible
// polynomial constant Rb is 0x87.
func cmacSubkeys(c cipher.Block) (k1, k2 []byte) {
	bs := c.BlockSize()
	// Rb depends on block size. We only use AES (bs=16) but be explicit.
	var rb byte
	switch bs {
	case 16:
		rb = 0x87
	case 8:
		rb = 0x1B
	default:
		// Unknown — fall back to AES constant. Callers in this package only
		// pass AES so this branch is never hit.
		rb = 0x87
	}

	l := make([]byte, bs)
	c.Encrypt(l, l) // L = AES_K(0^b)

	k1 = shiftLeftOne(l)
	if l[0]&0x80 != 0 {
		k1[bs-1] ^= rb
	}
	k2 = shiftLeftOne(k1)
	if k1[0]&0x80 != 0 {
		k2[bs-1] ^= rb
	}
	return k1, k2
}

// shiftLeftOne returns in << 1, treating in as a big-endian bitstring.
func shiftLeftOne(in []byte) []byte {
	out := make([]byte, len(in))
	var carry byte
	for i := len(in) - 1; i >= 0; i-- {
		out[i] = (in[i] << 1) | carry
		carry = (in[i] >> 7) & 0x01
	}
	return out
}

// cmac computes the full-length block-size AES-CMAC of msg under c. Output
// length equals the block size of c (16 bytes for AES).
//
// This follows NIST SP 800-38B "MAC Generation".
func cmac(c cipher.Block, msg []byte) []byte {
	return cmacWithSubkeys(c, msg, nil, nil)
}

// cmacWithSubkeys is cmac with pre-derived subkeys to avoid recomputing
// them on every call. Pass k1 == nil (or k2 == nil) to derive them inline.
func cmacWithSubkeys(c cipher.Block, msg, k1, k2 []byte) []byte {
	bs := c.BlockSize()
	if k1 == nil || k2 == nil {
		k1, k2 = cmacSubkeys(c)
	}

	// Step 2 of NIST SP 800-38B §6.2: choose n and identify if the last
	// block is complete. The empty-message case still goes through one
	// padded block.
	var (
		n        int
		complete bool
	)
	if len(msg) == 0 {
		n = 1
		complete = false
	} else {
		n = (len(msg) + bs - 1) / bs
		complete = len(msg)%bs == 0
	}

	// Step 4: build M_last.
	last := make([]byte, bs)
	if complete {
		copy(last, msg[(n-1)*bs:])
		for i := 0; i < bs; i++ {
			last[i] ^= k1[i]
		}
	} else {
		// Partial (or empty) last block: append 0x80 then 0x00s, XOR K2.
		off := (n - 1) * bs
		tail := msg[off:]
		copy(last, tail)
		last[len(tail)] = 0x80
		for i := 0; i < bs; i++ {
			last[i] ^= k2[i]
		}
	}

	// Step 5–6: CBC-MAC chain.
	x := make([]byte, bs) // X_0 = 0^b
	tmp := make([]byte, bs)
	for i := 0; i < n-1; i++ {
		for j := 0; j < bs; j++ {
			tmp[j] = msg[i*bs+j] ^ x[j]
		}
		c.Encrypt(x, tmp)
	}
	for j := 0; j < bs; j++ {
		tmp[j] = last[j] ^ x[j]
	}
	out := make([]byte, bs)
	c.Encrypt(out, tmp)
	return out
}
