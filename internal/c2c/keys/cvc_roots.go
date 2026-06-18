package keys

import (
	"math/big"
	"time"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/cvcert"
)

// CVC-Root trust anchors for the gematik card-verifiable-certificate PKI.
//
// These are a DIFFERENT trust hierarchy from the X.509 TLS/PKI roots in
// roots.go. CV-certs (BSI TR-03110, tag 7F21) chain only to CVC roots; the
// X.509 RCA chain is used for TLS / TSL signature / X.509 leaf certs. The
// two PKIs are parallel and have no cryptographic cross-link.
//
// Source: Atos/Eviden operator portal at https://cvc.egk-tsp.de.atos.net/
// (the gematik-approved CVC-Root TSP per gemProdT_CVC_Root_ECC). All keys
// are Brainpool P-256r1 / ECDSA-SHA-256 / role OID 1.2.276.0.76.4.152.
//
// **Trust note.** The spec PDFs (gemSpec_PKI, gemSpec_CVC_Root,
// gemSpec_CVC_TSP) describe the PKI process but do NOT embed the key
// bytes. Authoritative bytes come from the operator portal above, served
// over publicly-trusted TLS, with no detached signature. We mitigate by:
//   1. Pinning each root by SHA-256 of the source CVC DER (the
//      Fingerprint field).
//   2. Verifying each root's self-signature with our own brainpool ECDSA
//      stack at init() time (see TestCVCRootsSelfSign in cvc_roots_test.go).
//
// Full provenance and reproduction commands are in
// docs/c2c/cvc-root-research.md.

func brainpoolP256(xHex, yHex string) *cvcert.ECCPublicKey {
	x := new(big.Int)
	if _, ok := x.SetString(xHex, 16); !ok {
		panic("keys: bad CVC-Root X coordinate")
	}
	y := new(big.Int)
	if _, ok := y.SetString(yHex, 16); !ok {
		panic("keys: bad CVC-Root Y coordinate")
	}
	return &cvcert.ECCPublicKey{Curve: cvcert.AlgBrainpoolP256r1, X: x, Y: y}
}

// productionCVCRoots holds every gematik production CVC-Root ever issued
// (PU hierarchy, CHR prefix "DEZGW"). The current active root is the
// "Aktiv" one in the table at the top of the slice; older entries are
// retained for cards still in the field that haven't migrated.
var productionCVCRoots = []Root{
	// 7th Root — DEZGW870226 — current active production trust anchor
	{
		Name:        "gematik.CVC-Root.PU.7.DEZGW870226",
		KeyAlg:      cvcert.AlgBrainpoolP256r1,
		PublicKey:   brainpoolP256("10ae4964dc71ecd41f937a303ce302e84a3305e7f9606cedfecae0d99983bc8c", "7c9ce8a2d46141bbdc83ed356f2cf6534ca2473ca676e0bbae69129bac6a3d6e"),
		SubjectDN:   "CHR=DEZGW870226",
		NotBefore:   time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC),
		NotAfter:    time.Date(2036, 4, 14, 23, 59, 59, 0, time.UTC),
		Source:      "https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1776410492215/1776410492215_1",
		Fingerprint: "364d3a85fad1dbca9c4d8e1a2b9d07c61e4343abafe79dae38563887cf604a98",
		Notes:       "Aktiv production. Cross-cert signed by 6th Root DEZGW860224 available.",
	},
	// TODO: embed inactive production predecessors (DEZGW810214 …
	// DEZGW860224) when their X/Y coordinates have been extracted directly
	// from the source DERs at cvc.egk-tsp.de.atos.net. The research log
	// records SHA-256 fingerprints for all 7 generations but full X/Y only
	// for the active 7th — pulling the older bytes is a follow-up.
}

// testCVCRoots holds the gematik test CVC-Roots (RU and TU environments
// share a single hierarchy; CHR prefix "DEGXX"). These MUST NOT be
// configured as production trust anchors.
var testCVCRoots = []Root{
	// 8th Root — DEGXX890225 — current active test trust anchor
	{
		Name:        "gematik.CVC-Root.RU-TU.8.DEGXX890225",
		KeyAlg:      cvcert.AlgBrainpoolP256r1,
		PublicKey:   brainpoolP256("124d7746f3bf7b739976d28c7adb4d12d31bd665f1c00b4e48de3ade0e7eb31e", "325f2aa2d590076279c4300e88dc0f9fad1972eabf4a9b327eed3f76723bdd5c"),
		SubjectDN:   "CHR=DEGXX890225",
		NotBefore:   time.Date(2025, 12, 10, 0, 0, 0, 0, time.UTC),
		NotAfter:    time.Date(2035, 12, 9, 23, 59, 59, 0, time.UTC),
		Source:      "https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1766027554972/1766027554972_1",
		Fingerprint: "417d4bbfcf58a33ecce65fb513d7a2c0735106baa0e91a269865edd275db40f0",
		Notes:       "Aktiv test (RU+TU). Cross-cert signed by 7th Root DEGXX880224 available.",
	},
	{
		Name:        "gematik.CVC-Root.RU-TU.7.DEGXX880224",
		KeyAlg:      cvcert.AlgBrainpoolP256r1,
		PublicKey:   brainpoolP256("2881fc603f2a4f9ffa47d4950d33d85955a3a12de9a918c6201fa800433a76f1", "9fb6007a58e060376dcdee721081cc13a546ac6b4d02fd00ec16f6c9bef92de1"),
		SubjectDN:   "CHR=DEGXX880224",
		NotBefore:   time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC),
		NotAfter:    time.Date(2034, 1, 10, 23, 59, 59, 0, time.UTC),
		Source:      "https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1705926940362/1705926940362_1",
		Fingerprint: "36bf5c3aed6345aad84dc0ad12da2c3b1dac0456c3f1f4ffcb66804286880621",
		Notes:       "Inactive — superseded by 8th gen 2025-12-10.",
	},
	{
		Name:        "gematik.CVC-Root.RU-TU.6.DEGXX870222",
		KeyAlg:      cvcert.AlgBrainpoolP256r1,
		PublicKey:   brainpoolP256("015497bb49f7f3a639179ad2d4d6943bb10dbec480cd3c213d8012f71065d557", "9420d6fa4129aa67e04033507dc9bef283775f66dd470bf6498a3d23f35f27e1"),
		SubjectDN:   "CHR=DEGXX870222",
		NotBefore:   time.Date(2022, 1, 19, 0, 0, 0, 0, time.UTC),
		NotAfter:    time.Date(2032, 1, 18, 23, 59, 59, 0, time.UTC),
		Source:      "https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664358085028/1664358085028_1",
		Fingerprint: "bf183cdaa843b00f272daf2ba71a895f385d633fb19e73e7b46fdfcdabb657d9",
	},
}

// ProductionCVCRoots returns the trust anchors for the gematik production
// CVC PKI (CHR prefix "DEZGW"). Use these when validating CV-cert chains
// from a real-issued (Leistungserbringer) SMC-B or HBA.
func ProductionCVCRoots() []Root {
	out := make([]Root, len(productionCVCRoots))
	copy(out, productionCVCRoots)
	return out
}

// TestCVCRoots returns the trust anchors for the gematik RU/TU CVC PKI
// (CHR prefix "DEGXX"). Use these for development against a TI test card.
// MUST NOT be combined with production roots for real workflow.
func TestCVCRoots() []Root {
	out := make([]Root, len(testCVCRoots))
	copy(out, testCVCRoots)
	return out
}

// AllCVCRoots concatenates production + test CVC roots. Useful for
// diagnostic probing when you don't know which environment a card belongs
// to; never use it for production card validation.
func AllCVCRoots() []Root {
	out := make([]Root, 0, len(productionCVCRoots)+len(testCVCRoots))
	out = append(out, productionCVCRoots...)
	out = append(out, testCVCRoots...)
	return out
}
