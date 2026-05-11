package brainpool

import (
	"math/big"
	"testing"
)

// TestRFC7027_brainpoolP256r1_ECDH validates point arithmetic against
// the published ECDH test vector in RFC 7027 §A.1 "brainpoolP256r1".
//
// RFC 7027 — "Elliptic Curve Cryptography (ECC) Brainpool Curves for
// Transport Layer Security (TLS)", October 2013, Appendix A.1.
//
// The vector specifies two key pairs (dA, QA) and (dB, QB) and the
// shared secret Z = dA*QB = dB*QA. Verifying that our ScalarMult
// computes the same Q from d*G and the same Z from d*Q is an
// independent, third-party check of the curve parameters AND the
// scalar multiplication implementation.
func TestRFC7027_brainpoolP256r1_ECDH(t *testing.T) {
	c := P256r1()
	dA := mustHex("81DB1EE100150FF2EA338D708271BE38300CB54241D79950F77B063039804F1D")
	xQA := mustHex("44106E913F92BC02A1705D9953A8414DB95E1AAA49E81D9E85F929A8E3100BE5")
	yQA := mustHex("8AB4846F11CACCB73CE49CBDD120F5A900A69FD32C272223F789EF10EB089BDC")
	dB := mustHex("55E40BC41E37E3E2AD25C3C6654511FFA8474A91A0032087593852D3E7D76BD3")
	xQB := mustHex("8D2D688C6CF93E1160AD04CC4429117DC2C41825E1E9FCA0ADDD34E6F1B39F7B")
	yQB := mustHex("990C57520812BE512641E47034832106BC7D3E8DD0E4C7F1136D7006547CEC6A")
	xZ := mustHex("89AFC39D41D3B327814B80940B042590F96556EC91E6AE7939BCE31F3A18BF2B")

	// Check QA = dA*G.
	gxA, gyA := c.ScalarBaseMult(dA.Bytes())
	if gxA.Cmp(xQA) != 0 || gyA.Cmp(yQA) != 0 {
		t.Fatalf("dA*G mismatch\n got x=%x\n     y=%x\n want x=%x\n      y=%x",
			gxA, gyA, xQA, yQA)
	}
	// Check QB = dB*G.
	gxB, gyB := c.ScalarBaseMult(dB.Bytes())
	if gxB.Cmp(xQB) != 0 || gyB.Cmp(yQB) != 0 {
		t.Fatalf("dB*G mismatch\n got x=%x\n want x=%x", gxB, xQB)
	}
	// Check the shared secret both ways: dA*QB and dB*QA.
	zA_x, _ := c.ScalarMult(xQB, yQB, dA.Bytes())
	if zA_x.Cmp(xZ) != 0 {
		t.Fatalf("dA*QB x mismatch\n got %x\n want %x", zA_x, xZ)
	}
	zB_x, _ := c.ScalarMult(xQA, yQA, dB.Bytes())
	if zB_x.Cmp(xZ) != 0 {
		t.Fatalf("dB*QA x mismatch\n got %x\n want %x", zB_x, xZ)
	}
}

func mustHex(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("bad hex: " + s)
	}
	return n
}
