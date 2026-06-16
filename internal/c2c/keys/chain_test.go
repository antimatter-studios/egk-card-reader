package keys

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"hash"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/brainpool"
	"github.com/antimatter-studios/egk-card-reader/internal/c2c/cvcert"
)

// --- Brainpool adapter for crypto/ecdsa (mirrors brainpool_test.go) ------

type asEllipticCurve struct{ c *brainpool.Curve }

func (a asEllipticCurve) Params() *elliptic.CurveParams {
	return &elliptic.CurveParams{
		P:       a.c.P,
		N:       a.c.N,
		B:       a.c.B,
		Gx:      a.c.Gx,
		Gy:      a.c.Gy,
		BitSize: a.c.BitSize,
		Name:    a.c.Name,
	}
}
func (a asEllipticCurve) IsOnCurve(x, y *big.Int) bool { return a.c.IsOnCurve(x, y) }
func (a asEllipticCurve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	x1a, y1a := fromZero(x1, y1)
	x2a, y2a := fromZero(x2, y2)
	ox, oy := a.c.Add(x1a, y1a, x2a, y2a)
	return toZero(ox, oy)
}
func (a asEllipticCurve) Double(x, y *big.Int) (*big.Int, *big.Int) {
	xa, ya := fromZero(x, y)
	ox, oy := a.c.Double(xa, ya)
	return toZero(ox, oy)
}
func (a asEllipticCurve) ScalarMult(x, y *big.Int, k []byte) (*big.Int, *big.Int) {
	xa, ya := fromZero(x, y)
	ox, oy := a.c.ScalarMult(xa, ya, k)
	return toZero(ox, oy)
}
func (a asEllipticCurve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) {
	ox, oy := a.c.ScalarBaseMult(k)
	return toZero(ox, oy)
}

func fromZero(x, y *big.Int) (*big.Int, *big.Int) {
	if x == nil || y == nil {
		return nil, nil
	}
	if x.Sign() == 0 && y.Sign() == 0 {
		return nil, nil
	}
	return x, y
}

func toZero(x, y *big.Int) (*big.Int, *big.Int) {
	if x == nil || y == nil {
		return new(big.Int), new(big.Int)
	}
	return x, y
}

// --- Helpers to synthesize *cvcert.Cert + sign with test keys -----------

// signRSA returns a PKCS#1 v1.5 SHA-256 signature over body.
func signRSA(t *testing.T, priv *rsa.PrivateKey, body []byte) []byte {
	t.Helper()
	h := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	return sig
}

// signBrainpool returns a raw (r||s) ECDSA signature using crypto/ecdsa
// over the brainpool curve adapter. coordLen is N's byte length.
func signBrainpool(t *testing.T, priv *ecdsa.PrivateKey, h hash.Hash, body []byte, coordLen int) []byte {
	t.Helper()
	h.Write(body)
	digest := h.Sum(nil)
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest)
	if err != nil {
		t.Fatalf("ecdsa.Sign: %v", err)
	}
	out := make([]byte, 2*coordLen)
	rb := r.Bytes()
	sb := s.Bytes()
	copy(out[coordLen-len(rb):coordLen], rb)
	copy(out[2*coordLen-len(sb):], sb)
	return out
}

// makeRSACert builds a *cvcert.Cert with given CAR/CHR/validity, body
// bytes set to a deterministic test vector, and Signature produced by
// signing with parentPriv.
func makeRSACert(t *testing.T, car, chr string, nb, na time.Time, parentPriv *rsa.PrivateKey, selfPub *cvcert.RSAPublicKey) *cvcert.Cert {
	t.Helper()
	body := []byte("cvcert-body|car=" + car + "|chr=" + chr)
	sig := signRSA(t, parentPriv, body)
	return &cvcert.Cert{
		CAR:       car,
		CHR:       chr,
		NotBefore: nb,
		NotAfter:  na,
		KeyAlg:    cvcert.AlgRSA2048,
		PublicKey: selfPub,
		Body:      body,
		Signature: sig,
	}
}

func makeECCCert(t *testing.T, car, chr string, nb, na time.Time, parentPriv *ecdsa.PrivateKey, parentHash func() hash.Hash, parentCoord int, alg cvcert.KeyAlg, selfPub *cvcert.ECCPublicKey) *cvcert.Cert {
	t.Helper()
	body := []byte("cvcert-body|car=" + car + "|chr=" + chr)
	sig := signBrainpool(t, parentPriv, parentHash(), body, parentCoord)
	return &cvcert.Cert{
		CAR:       car,
		CHR:       chr,
		NotBefore: nb,
		NotAfter:  na,
		KeyAlg:    alg,
		PublicKey: selfPub,
		Body:      body,
		Signature: sig,
	}
}

// --- Tests --------------------------------------------------------------

// TestVerifyChain_RSAHappyPath builds a 2-level chain
// (leaf SMC-B → CA → root) using RSA-2048 throughout and verifies it.
func TestVerifyChain_RSAHappyPath(t *testing.T) {
	rootPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey root: %v", err)
	}
	caPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey ca: %v", err)
	}
	leafPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey leaf: %v", err)
	}

	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nb := now.Add(-365 * 24 * time.Hour)
	na := now.Add(365 * 24 * time.Hour)

	leafPub := &cvcert.RSAPublicKey{N: leafPriv.N, E: big.NewInt(int64(leafPriv.E))}
	caPub := &cvcert.RSAPublicKey{N: caPriv.N, E: big.NewInt(int64(caPriv.E))}

	// chain[0] (leaf) signed by ca
	leaf := makeRSACert(t, "TEST_CA", "TEST_LEAF", nb, na, caPriv, leafPub)
	// chain[1] (ca) signed by root
	ca := makeRSACert(t, "TEST_ROOT", "TEST_CA", nb, na, rootPriv, caPub)

	root := Root{
		Name:      "TEST_ROOT",
		KeyAlg:    cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: rootPriv.N, E: big.NewInt(int64(rootPriv.E))},
	}

	if err := VerifyChain([]*cvcert.Cert{leaf, ca}, []Root{root}, now); err != nil {
		t.Fatalf("VerifyChain: unexpected error: %v", err)
	}
}

// TestVerifyChain_RejectsExpired runs the same chain but at a time after
// notAfter and expects an "expired" error.
func TestVerifyChain_RejectsExpired(t *testing.T) {
	rootPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	nb := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	na := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	caPub := &cvcert.RSAPublicKey{N: caPriv.N, E: big.NewInt(int64(caPriv.E))}
	ca := makeRSACert(t, "TEST_ROOT", "TEST_CA", nb, na, rootPriv, caPub)
	root := Root{Name: "TEST_ROOT", KeyAlg: cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: rootPriv.N, E: big.NewInt(int64(rootPriv.E))}}

	err := VerifyChain([]*cvcert.Cert{ca}, []Root{root}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected expiry error, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expiry error, got: %v", err)
	}
}

// TestVerifyChain_RejectsNotYetValid: at time before NotBefore.
func TestVerifyChain_RejectsNotYetValid(t *testing.T) {
	rootPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	nb := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	na := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	caPub := &cvcert.RSAPublicKey{N: caPriv.N, E: big.NewInt(int64(caPriv.E))}
	ca := makeRSACert(t, "TEST_ROOT", "TEST_CA", nb, na, rootPriv, caPub)
	root := Root{Name: "TEST_ROOT", KeyAlg: cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: rootPriv.N, E: big.NewInt(int64(rootPriv.E))}}

	err := VerifyChain([]*cvcert.Cert{ca}, []Root{root}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected not-yet-valid error, got nil")
	}
	if !strings.Contains(err.Error(), "not yet valid") {
		t.Fatalf("expected not-yet-valid error, got: %v", err)
	}
}

// TestVerifyChain_RejectsNameMismatch: CAR/CHR mismatch between levels.
func TestVerifyChain_RejectsNameMismatch(t *testing.T) {
	rootPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	leafPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Now().UTC()
	nb := now.Add(-time.Hour)
	na := now.Add(time.Hour)
	leafPub := &cvcert.RSAPublicKey{N: leafPriv.N, E: big.NewInt(int64(leafPriv.E))}
	caPub := &cvcert.RSAPublicKey{N: caPriv.N, E: big.NewInt(int64(caPriv.E))}
	leaf := makeRSACert(t, "WRONG_CA", "TEST_LEAF", nb, na, caPriv, leafPub) // CAR != ca.CHR
	ca := makeRSACert(t, "TEST_ROOT", "TEST_CA", nb, na, rootPriv, caPub)
	root := Root{Name: "TEST_ROOT", KeyAlg: cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: rootPriv.N, E: big.NewInt(int64(rootPriv.E))}}

	err := VerifyChain([]*cvcert.Cert{leaf, ca}, []Root{root}, now)
	if err == nil {
		t.Fatal("expected CAR/CHR mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatch error, got: %v", err)
	}
}

// TestVerifyChain_RejectsUntrustedRoot: ca chains to a root not in the trust set.
func TestVerifyChain_RejectsUntrustedRoot(t *testing.T) {
	rootPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Now().UTC()
	nb := now.Add(-time.Hour)
	na := now.Add(time.Hour)
	caPub := &cvcert.RSAPublicKey{N: caPriv.N, E: big.NewInt(int64(caPriv.E))}
	ca := makeRSACert(t, "TEST_ROOT", "TEST_CA", nb, na, rootPriv, caPub)
	// Trust set contains a different root.
	otherPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	root := Root{Name: "OTHER_ROOT", KeyAlg: cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: otherPriv.N, E: big.NewInt(int64(otherPriv.E))}}

	err := VerifyChain([]*cvcert.Cert{ca}, []Root{root}, now)
	if err == nil {
		t.Fatal("expected untrusted-root error, got nil")
	}
	if !strings.Contains(err.Error(), "does not match any trusted root") {
		t.Fatalf("expected untrusted root error, got: %v", err)
	}
}

// TestVerifyChain_RejectsBadSignature: tamper a signature byte.
func TestVerifyChain_RejectsBadSignature(t *testing.T) {
	rootPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Now().UTC()
	nb := now.Add(-time.Hour)
	na := now.Add(time.Hour)
	caPub := &cvcert.RSAPublicKey{N: caPriv.N, E: big.NewInt(int64(caPriv.E))}
	ca := makeRSACert(t, "TEST_ROOT", "TEST_CA", nb, na, rootPriv, caPub)
	ca.Signature[0] ^= 0x01
	root := Root{Name: "TEST_ROOT", KeyAlg: cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: rootPriv.N, E: big.NewInt(int64(rootPriv.E))}}

	err := VerifyChain([]*cvcert.Cert{ca}, []Root{root}, now)
	if err == nil {
		t.Fatal("expected bad-signature error, got nil")
	}
	if !errors.Is(err, rsa.ErrVerification) && !strings.Contains(err.Error(), "verification error") {
		t.Fatalf("expected RSA verification error, got: %v", err)
	}
}

// TestVerifyChain_BrainpoolP256: parent is brainpoolP256r1, leaf is RSA.
// Exercises the ECC verification path inside verifySignature.
func TestVerifyChain_BrainpoolP256(t *testing.T) {
	ec := asEllipticCurve{c: brainpool.P256r1()}
	caPriv, err := ecdsa.GenerateKey(ec, rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	leafPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Now().UTC()
	nb := now.Add(-time.Hour)
	na := now.Add(time.Hour)
	leafPub := &cvcert.RSAPublicKey{N: leafPriv.N, E: big.NewInt(int64(leafPriv.E))}

	// Sign the leaf with the CA's ECDSA key (raw r||s, 32 bytes each).
	body := []byte("cvcert-body|car=TEST_CA|chr=TEST_LEAF")
	rH := sha256.New()
	rH.Write(body)
	digest := rH.Sum(nil)
	r, s, err := ecdsa.Sign(rand.Reader, caPriv, digest)
	if err != nil {
		t.Fatalf("ecdsa.Sign: %v", err)
	}
	sig := make([]byte, 64)
	rb, sb := r.Bytes(), s.Bytes()
	copy(sig[32-len(rb):32], rb)
	copy(sig[64-len(sb):], sb)

	leaf := &cvcert.Cert{
		CAR:       "TEST_CA",
		CHR:       "TEST_LEAF",
		NotBefore: nb,
		NotAfter:  na,
		KeyAlg:    cvcert.AlgRSA2048,
		PublicKey: leafPub,
		Body:      body,
		Signature: sig,
	}
	caCert := &cvcert.Cert{
		CAR:       "TEST_ROOT",
		CHR:       "TEST_CA",
		NotBefore: nb,
		NotAfter:  na,
		KeyAlg:    cvcert.AlgBrainpoolP256r1,
		PublicKey: &cvcert.ECCPublicKey{Curve: cvcert.AlgBrainpoolP256r1, X: caPriv.X, Y: caPriv.Y},
		Body:      []byte("ca-body"),
		Signature: []byte{}, // not verified — its parent is the root, see below
	}

	// We don't want to require an ECC root here; sign the CA cert with an
	// RSA test root so we can mount it as the trust anchor.
	rootPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caBody := caCert.Body
	hb := sha256.Sum256(caBody)
	caSig, err := rsa.SignPKCS1v15(rand.Reader, rootPriv, crypto.SHA256, hb[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	caCert.Signature = caSig
	root := Root{
		Name:      "TEST_ROOT",
		KeyAlg:    cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: rootPriv.N, E: big.NewInt(int64(rootPriv.E))},
	}

	if err := VerifyChain([]*cvcert.Cert{leaf, caCert}, []Root{root}, now); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
}

// TestVerifyChain_BrainpoolP384End2End: ca uses brainpoolP384r1, leaf
// signed by it; root is also brainpoolP384r1 (self-signs caCert via the
// same key — the test only checks that verifyBrainpool dispatch works
// for P384 + SHA-384).
func TestVerifyChain_BrainpoolP384(t *testing.T) {
	curve := brainpool.P384r1()
	ec := asEllipticCurve{c: curve}
	rootPriv, err := ecdsa.GenerateKey(ec, rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	now := time.Now().UTC()
	nb := now.Add(-time.Hour)
	na := now.Add(time.Hour)

	caCert := makeECCCert(t,
		"TEST_ROOT_P384", "TEST_CA_P384",
		nb, na,
		rootPriv,
		sha512.New384,
		48,
		cvcert.AlgBrainpoolP384r1,
		&cvcert.ECCPublicKey{Curve: cvcert.AlgBrainpoolP384r1, X: rootPriv.X, Y: rootPriv.Y},
	)
	root := Root{
		Name:      "TEST_ROOT_P384",
		KeyAlg:    cvcert.AlgBrainpoolP384r1,
		PublicKey: &cvcert.ECCPublicKey{Curve: cvcert.AlgBrainpoolP384r1, X: rootPriv.X, Y: rootPriv.Y},
	}

	if err := VerifyChain([]*cvcert.Cert{caCert}, []Root{root}, now); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
}

// TestVerifyChain_EmptyInputs covers degenerate inputs.
func TestVerifyChain_EmptyInputs(t *testing.T) {
	if err := VerifyChain(nil, []Root{{Name: "x"}}, time.Now()); err == nil {
		t.Fatal("empty chain accepted")
	}
	if err := VerifyChain([]*cvcert.Cert{{}}, nil, time.Now()); err == nil {
		t.Fatal("empty roots accepted")
	}
}

// TestVerifyChain_SimulatedSlot2_ChainRoutesToTestRoot demonstrates the
// fixture pattern called out in the task brief: synthesize a
// *cvcert.Cert tree whose CAR/CHR names mirror the real slot-2 SMC-B
// X.509 hierarchy (leaf → GEM.SMCB-CA24 TEST-ONLY → gematik.RCA2.TEST-ONLY)
// and confirm the chain walker picks the right root *by name*. The
// crypto on this synthesized chain uses our own RSA test keys (we
// can't synthesize a signature under gematik's real private key), so
// this test does NOT prove any cryptographic property of the real
// slot-2 cert — only that the chain-walking + name-routing logic
// terminates at the expected gematik test root entry.
func TestVerifyChain_SimulatedSlot2_ChainRoutesToTestRoot(t *testing.T) {
	// Look up the real root entry for name resolution. We re-host its
	// Name + KeyAlg but substitute a test public key so we can produce
	// a signature it will verify.
	realRoots := TestRoots()
	var rca2 Root
	for _, r := range realRoots {
		if r.Name == "gematik.RCA2.TEST-ONLY" {
			rca2 = r
			break
		}
	}
	if rca2.Name == "" {
		t.Fatal("gematik.RCA2.TEST-ONLY not in TestRoots")
	}

	rootPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	leafPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	nb := now.Add(-365 * 24 * time.Hour)
	na := now.Add(365 * 24 * time.Hour)
	leafPub := &cvcert.RSAPublicKey{N: leafPriv.N, E: big.NewInt(int64(leafPriv.E))}
	caPub := &cvcert.RSAPublicKey{N: caPriv.N, E: big.NewInt(int64(caPriv.E))}

	// Names mirror the real slot-2 cert hierarchy.
	leaf := makeRSACert(t,
		"GEM.SMCB-CA24 TEST-ONLY",
		"Praxis Beutlín TEST-ONLY",
		nb, na, caPriv, leafPub,
	)
	ca := makeRSACert(t,
		"gematik.RCA2.TEST-ONLY",
		"GEM.SMCB-CA24 TEST-ONLY",
		nb, na, rootPriv, caPub,
	)

	// Shadow the real root entry with one whose public key matches our
	// rootPriv but whose Name is still "gematik.RCA2.TEST-ONLY".
	rca2Test := Root{
		Name:      rca2.Name,
		KeyAlg:    cvcert.AlgRSA2048,
		PublicKey: &cvcert.RSAPublicKey{N: rootPriv.N, E: big.NewInt(int64(rootPriv.E))},
	}

	// Trust set includes ALL test roots, but only gematik.RCA2.TEST-ONLY
	// matches CAR — and we replace its key with our test key so the
	// crypto succeeds. This proves the walker selects the right Root by
	// name, even when other roots are present.
	roots := []Root{
		rca2Test,
		{Name: "gematik.RCA6.TEST-ONLY", KeyAlg: cvcert.AlgRSA2048, PublicKey: &cvcert.RSAPublicKey{N: big.NewInt(3), E: big.NewInt(65537)}},
		{Name: "gematik.RCA9.TEST-ONLY", KeyAlg: cvcert.AlgRSA2048, PublicKey: &cvcert.RSAPublicKey{N: big.NewInt(5), E: big.NewInt(65537)}},
	}

	if err := VerifyChain([]*cvcert.Cert{leaf, ca}, roots, now); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}

	// And confirm: against the REAL (untouched) test roots, the same
	// chain must FAIL — our caBody was signed with a fresh RSA key,
	// not gematik's RCA2 TEST-ONLY private key. This is the second
	// half of the contract: real roots reject fake chains.
	if err := VerifyChain([]*cvcert.Cert{leaf, ca}, realRoots, now); err == nil {
		t.Fatal("synthesized chain unexpectedly verified against real gematik test roots")
	}
}

