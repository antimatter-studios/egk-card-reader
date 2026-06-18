package keys

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"time"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/brainpool"
	"github.com/antimatter-studios/egk-card-reader/internal/c2c/cvcert"
)

// VerifyChain walks chain[0] (leaf) → chain[1] → ... → chain[n-1] (closest
// to the root) and verifies each link's signature.
//
// For each i in [0, n-1):
//   - chain[i].CAR must equal chain[i+1].CHR (the issuer of chain[i] is
//     the holder of chain[i+1]).
//   - chain[i+1].PublicKey must verify chain[i].Signature over chain[i].Body.
//
// The final certificate chain[n-1] must be issued by a trusted Root:
//   - chain[n-1].CAR must equal some root.Name in `roots`.
//   - That root.PublicKey must verify chain[n-1].Signature over chain[n-1].Body.
//
// Time-validity: every cert's NotBefore <= at <= NotAfter. The roots' own
// validity is not enforced.
//
// Algorithm dispatch is driven by the verifying key's algorithm
// (chain[i+1].KeyAlg, or root.KeyAlg for the final step):
//   - AlgRSA2048             → PKCS#1 v1.5 with SHA-256
//   - AlgBrainpoolP256r1     → ECDSA(SHA-256) over brainpoolP256r1
//   - AlgBrainpoolP384r1     → ECDSA(SHA-384) over brainpoolP384r1
//   - AlgBrainpoolP512r1     → ECDSA(SHA-512) over brainpoolP512r1
//
// Returns nil on success, or a wrapped error describing the first failure.
func VerifyChain(chain []*cvcert.Cert, roots []Root, at time.Time) error {
	if len(chain) == 0 {
		return errors.New("keys.VerifyChain: empty chain")
	}
	if len(roots) == 0 {
		return errors.New("keys.VerifyChain: no trust anchors supplied")
	}

	// 1. Time-validity on every cert.
	for i, c := range chain {
		if c == nil {
			return fmt.Errorf("keys.VerifyChain: chain[%d] is nil", i)
		}
		if err := checkValidity(c, at, i); err != nil {
			return err
		}
		if len(c.Body) == 0 || len(c.Signature) == 0 {
			return fmt.Errorf("keys.VerifyChain: chain[%d] missing Body/Signature (CHR=%q)", i, c.CHR)
		}
	}

	// 2. Verify chain[i] using chain[i+1]'s public key, plus name-binding.
	for i := 0; i < len(chain)-1; i++ {
		child := chain[i]
		parent := chain[i+1]
		if child.CAR != parent.CHR {
			return fmt.Errorf("keys.VerifyChain: chain[%d].CAR=%q does not match chain[%d].CHR=%q",
				i, child.CAR, i+1, parent.CHR)
		}
		if err := verifySignature(parent.KeyAlg, parent.PublicKey, child.Body, child.Signature); err != nil {
			return fmt.Errorf("keys.VerifyChain: chain[%d] signature by %q invalid: %w", i, parent.CHR, err)
		}
	}

	// 3. Verify the final cert against a trusted root.
	last := chain[len(chain)-1]
	root, ok := findRoot(roots, last.CAR)
	if !ok {
		return fmt.Errorf("keys.VerifyChain: chain[%d].CAR=%q does not match any trusted root", len(chain)-1, last.CAR)
	}
	if err := verifySignature(root.KeyAlg, root.PublicKey, last.Body, last.Signature); err != nil {
		return fmt.Errorf("keys.VerifyChain: root %q failed to verify chain[%d] (CHR=%q): %w",
			root.Name, len(chain)-1, last.CHR, err)
	}
	return nil
}

// checkValidity returns an error if at lies outside [c.NotBefore, c.NotAfter].
func checkValidity(c *cvcert.Cert, at time.Time, idx int) error {
	if !c.NotBefore.IsZero() && at.Before(c.NotBefore) {
		return fmt.Errorf("keys.VerifyChain: chain[%d] not yet valid (NotBefore=%s, at=%s, CHR=%q)",
			idx, c.NotBefore.Format(time.RFC3339), at.Format(time.RFC3339), c.CHR)
	}
	if !c.NotAfter.IsZero() && at.After(c.NotAfter) {
		return fmt.Errorf("keys.VerifyChain: chain[%d] expired (NotAfter=%s, at=%s, CHR=%q)",
			idx, c.NotAfter.Format(time.RFC3339), at.Format(time.RFC3339), c.CHR)
	}
	return nil
}

// findRoot looks up a root by Name. Returns the root and true if found.
func findRoot(roots []Root, name string) (Root, bool) {
	for _, r := range roots {
		if r.Name == name {
			return r, true
		}
	}
	return Root{}, false
}

// verifySignature dispatches to the right primitive based on the
// verifying key's algorithm. pub must be of the type matching alg
// (*cvcert.RSAPublicKey for AlgRSA2048, *cvcert.ECCPublicKey for the
// Brainpool variants). body is hashed; sig is the raw signature bytes
// as returned by cvcert (PKCS#1 v1.5 block for RSA, plain (r||s)
// concatenation for ECDSA).
func verifySignature(alg cvcert.KeyAlg, pub any, body, sig []byte) error {
	switch alg {
	case cvcert.AlgRSA2048:
		rsaKey, ok := pub.(*cvcert.RSAPublicKey)
		if !ok {
			return fmt.Errorf("RSA-2048 selected but public key is %T", pub)
		}
		return verifyRSAPKCS1v15(rsaKey, sha256.New(), crypto.SHA256, body, sig)
	case cvcert.AlgBrainpoolP256r1:
		eccKey, ok := pub.(*cvcert.ECCPublicKey)
		if !ok {
			return fmt.Errorf("Brainpool P256r1 selected but public key is %T", pub)
		}
		return verifyBrainpool(eccKey, brainpool.P256r1(), 32, sha256.New(), body, sig)
	case cvcert.AlgBrainpoolP384r1:
		eccKey, ok := pub.(*cvcert.ECCPublicKey)
		if !ok {
			return fmt.Errorf("Brainpool P384r1 selected but public key is %T", pub)
		}
		return verifyBrainpool(eccKey, brainpool.P384r1(), 48, sha512.New384(), body, sig)
	case cvcert.AlgBrainpoolP512r1:
		eccKey, ok := pub.(*cvcert.ECCPublicKey)
		if !ok {
			return fmt.Errorf("Brainpool P512r1 selected but public key is %T", pub)
		}
		return verifyBrainpool(eccKey, brainpool.P512r1(), 64, sha512.New(), body, sig)
	default:
		return fmt.Errorf("unsupported key algorithm %d", alg)
	}
}

// verifyRSAPKCS1v15 hashes body and runs crypto/rsa.VerifyPKCS1v15.
func verifyRSAPKCS1v15(pk *cvcert.RSAPublicKey, h hash.Hash, hashID crypto.Hash, body, sig []byte) error {
	if pk == nil || pk.N == nil || pk.E == nil {
		return errors.New("RSA public key is nil or incomplete")
	}
	if !pk.E.IsInt64() {
		return errors.New("RSA exponent does not fit in int")
	}
	e64 := pk.E.Int64()
	if e64 <= 0 || e64 > (1<<31-1) {
		return fmt.Errorf("RSA exponent out of range: %d", e64)
	}
	stdKey := &rsa.PublicKey{N: new(big.Int).Set(pk.N), E: int(e64)}
	h.Write(body)
	digest := h.Sum(nil)
	if err := rsa.VerifyPKCS1v15(stdKey, hashID, digest, sig); err != nil {
		return err
	}
	return nil
}

// verifyBrainpool decodes a raw ECDSA signature (r||s, each `coordLen`
// big-endian bytes) and verifies it via the brainpool package.
func verifyBrainpool(pk *cvcert.ECCPublicKey, curve *brainpool.Curve, coordLen int, h hash.Hash, body, sig []byte) error {
	if pk == nil || pk.X == nil || pk.Y == nil {
		return errors.New("ECC public key is nil or incomplete")
	}
	if len(sig) != 2*coordLen {
		return fmt.Errorf("ECDSA signature length %d, want %d for %s", len(sig), 2*coordLen, curve.Name)
	}
	r := new(big.Int).SetBytes(sig[:coordLen])
	s := new(big.Int).SetBytes(sig[coordLen:])
	h.Write(body)
	digest := h.Sum(nil)
	if !curve.VerifyECDSA(pk.X, pk.Y, digest, r, s) {
		return errors.New("ECDSA verify failed")
	}
	return nil
}
