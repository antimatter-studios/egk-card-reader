package keys

import (
	"math/big"
	"time"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/cvcert"
)

// Root is a trust anchor for one gematik PKI root certificate.
//
// PublicKey is one of:
//   - *cvcert.RSAPublicKey for AlgRSA2048
//   - *cvcert.ECCPublicKey for AlgBrainpoolP{256,384,512}r1
//
// SubjectDN, NotBefore and NotAfter are informational. VerifyChain does
// not enforce the root's own validity — a root is trusted by configuration,
// not by its self-signed dates — but callers may inspect them to surface
// rotation warnings.
type Root struct {
	Name        string         // short stable identifier (e.g. "gematik.RCA6")
	KeyAlg      cvcert.KeyAlg  // AlgRSA2048 | AlgBrainpoolP*
	PublicKey   any            // *cvcert.RSAPublicKey or *cvcert.ECCPublicKey
	SubjectDN   string         // X.509 Subject DN of the source certificate
	NotBefore   time.Time      // validity bounds of the source certificate
	NotAfter    time.Time
	Source      string         // URL or document reference the cert was fetched from
	Fingerprint string         // lower-case SHA-256 hex of the source DER
	Notes       string         // free-form: production vs test, generation, successor relationships, ...
}

// rsa2048 builds a *cvcert.RSAPublicKey from a hex modulus and a small
// integer exponent. Panics on malformed input; all inputs are
// compile-time constants extracted from the embedded certificate DERs.
func rsa2048(hexN string, e int) *cvcert.RSAPublicKey {
	n := new(big.Int)
	if _, ok := n.SetString(hexN, 16); !ok {
		panic("keys: bad hex modulus")
	}
	return &cvcert.RSAPublicKey{N: n, E: big.NewInt(int64(e))}
}

const rsaCommonExponent = 65537

// --- Production roots ---------------------------------------------------
//
// All three moduli below were extracted with
//   openssl x509 -inform der -in GEM.RCA<N>.der -noout -modulus
// from the DER files published at
//   https://download.tsl.ti-dienste.de/ROOT-CA/GEM.RCA<N>.der
// Fingerprints are SHA-256 of the DER bytes; see sources.md for the full
// extraction log and dates fetched.

// GEM.RCA2: legacy production root, RSA-2048, valid 2016-12-09 → 2026-12-07.
// Still trusted by older eGK / SMC-B / HBA cards in the field.
const gemRCA2ModulusHex = "" +
	"ab5035ef7bb0cfc502f3aa35899d3e255bf8e4fe255edb11dad5d20e2fc048a8" +
	"3361e9797eeb144d1485b5a7a07bcd425824f02dacfab41c2b0aebbcfd94c985" +
	"8daabbeacbe6c90f2e7b4507969afb2a96db8e83d6c60263d6112ca9df0a8513" +
	"e5f96ab35025ad5abafa28f7ebb346e1a41d15f8709d1995baf530bba5001c36" +
	"e8edce8f14a6de37f8b97275664949ae404c891402c4c615c804a7b4e90deddf" +
	"933d914030fc5e25ff701700d4927caaea25d36676b2bdbd70b19d9193023f37" +
	"923b98ddce2157d5d713b32b5c6b44159425d03c58f970211a2ab6c1f86cee16" +
	"f027ecc932f0b6cae1938d443309b23b54efcbaa7b12f18c50da584e7771c005"

// GEM.RCA6: current production root, RSA-2048, valid 2021-11-11 → 2031-11-09.
const gemRCA6ModulusHex = "" +
	"c524b1ce4850b38aad4370f2619ebeb1a4aa38628d634d7027252f203b5bca31" +
	"c4c41b46cd12923996e2ccc48c3bc4101277fb88b8487c9dd6dcd92c4017f081" +
	"fa861b02b6d8b5c0fb791205efa0b1a0f68785d59fcfd8def64490930e34c020" +
	"b1d8f4896e2c1c75acc01d77d1453633407d06ed08bc0773d54ff98610285b0f" +
	"bad81c2d60cabd2dbc0176e32280897c966802397dc8c1883616b1adda3764ac" +
	"07579fac8b9dc81597d484a66b4a3c0921a55548986543a704556299cf240d13" +
	"0afab7dbe57cb2ee4ef49eb8b7b984a58d15dde3ed433ebd8517dbb594224529" +
	"79ced25455802ad3f6c521e27d2d87d461e604fa4d29502a2bc8a6bc5ca7ce9f"

// GEM.RCA9: next-generation production root, RSA-2048, valid 2025-06-04 → 2035-06-02.
const gemRCA9ModulusHex = "" +
	"dd1d7b2f2813b3328e8faf932062f93ca5077557608300f936da15a9730c435b" +
	"d2acabd87d2ba605199c045665ade48b449aca5344c22e639d2a3d5e60814b4b" +
	"729e697c6a008204a57afb1d45c3ea8d2fc8b31410b499761a250e44eb80e775" +
	"7152c5aa9ec55d51d57c42796dfceb617ec30f51248c6e2c413ae05bb64d7f97" +
	"4f9126cec5b50a0601b41b06b62dadd807e2f30983bd2082fe54606094949bf4" +
	"9eb1fc1db330238fd926e8f342f4491950a419e45fa47503167734ba3b4663f1" +
	"ca94fc97ac6452f4fa7a82d478b6a62ae28f9c6d17aeaaf2d79b03d2a08ab2bd" +
	"c73b7b72eeee78c8609a0000b182848875114c89e1aa501dc1019a4450041559"

// --- TEST-ONLY roots ----------------------------------------------------
//
// Fetched from https://download-test.tsl.ti-dienste.de/ROOT-CA/. These
// anchor the gematik test PKI. The slot-2 SMC-B in this project
// (issuer "GEM.SMCB-CA24 TEST-ONLY") chains to GEM.RCA2 TEST-ONLY.
// Real cards reject any chain rooted in a TEST-ONLY CA.

// GEM.RCA2 TEST-ONLY: legacy test root, RSA-2048, valid 2016-11-17 → 2026-11-15.
const gemRCA2TestModulusHex = "" +
	"ca604752e7e4cc4aa35ef0b110f096529f34bee93ca725d5bff20c9e002ef3b1" +
	"858d05badb162d7100d620269e2c4a162326c417f24a9babdb4829ea20560fe4" +
	"1b2b38f5cf63a079e976fa3dab71a92d805db631af42649792b51cf5822bb8d9" +
	"9587a9fed7e1c3e5e8fc8a183129036f409f35be4609825ab8837c94e18b6c78" +
	"8c83aa0c2dfc6f4e789003783871f773b1ba387fd900903671aa6a192ddba171" +
	"7bf7d60fc90dd76cd412208a2bb58946ebd8a398470d986b482f27c79cd91322" +
	"a02f3bbafcb36e87eebd204ae53e3da634ac8089deece28f9c1cf599f543d60d" +
	"fb21854d14e816c8e14a2a8654eebc98762cd635cb3fb7354009de5546a30f63"

// GEM.RCA6 TEST-ONLY, RSA-2048, valid 2021-10-28 → 2031-10-26.
const gemRCA6TestModulusHex = "" +
	"be741e88111f9d10fbc3386117b021d0b9d529d9bb5e48507eb55b7c87094a61" +
	"722165d8261aee8b7a1812b71504385c1221ea07c129b6fca23d6e131fbe8b4e" +
	"ad5e6baba5bebfcecddc2731cf0df518988e23ebe2b2e8ffc4813e8d1caf17c0" +
	"5242d59a7a248a91a4bfe0044ae09fcc598d9ddd43d6977f348ddd175fbe4166" +
	"75093e1595810bef7610928e03465c8854997f2cf3595f00a62269560a434005" +
	"520d457c8a846d800263982ab250d03842d7b1da753a0bc1dd632ad7d8a878c2" +
	"dbc91d3de540df48506380be59be97780005027aee63a94191e6afea2d83a3ae" +
	"0f95a73ef27cd88611f05387478a761ca73fc53779e1ceef3b797e6ac956355f"

// GEM.RCA9 TEST-ONLY, RSA-2048, valid 2025-05-08 → 2035-05-06.
const gemRCA9TestModulusHex = "" +
	"cfbf5653cb69b4553a143861497668ca42bb442587cb0bbb18fe3890727879ca" +
	"0685fb402dd2ce3b039b504175939191b315851d3baae8d0644d7e332a35e8ac" +
	"eb097a138a3f46006ba4e3387a4ecb459699a7205e41ac0e9aed66f0395f22bb" +
	"cd847cfd70090fe401719fd8f38950c5f6781e8968a89ebbe65e1cd5e2a7db8f" +
	"23b0c37ca3267735d9b1c81ddf72e6d9459266d2198828cd2c9a771702307d01" +
	"d26431998eae467eac0be9b1e0581ae86e0625768cf2c363814b604e039e10c6" +
	"24a2692a5d86a7175e92d7ca2dff433552d0218c2c49d0ca20019301146d1acf" +
	"b301dc71708a3525fd81c060ddea8265afcfd4692289bad186f9ad2804f9c02b"

// ProductionRoots returns gematik's currently-published production root
// trust anchors. The returned slice is a fresh copy; embedded big.Int
// pointers are shared and must be treated as read-only.
//
// Note on scope: these roots anchor the X.509 PKI used for the cards'
// certificates of approval (the "C." certs at SFI 1..7). The CV-cert
// chain presented during the live gemSpec_COS §13 C2C handshake
// terminates at a separate gematik CVC-Root, which is NOT published via
// the TSL endpoint and is NOT embedded here. See doc.go and sources.md.
func ProductionRoots() []Root {
	return []Root{
		{
			Name:        "gematik.RCA2",
			KeyAlg:      cvcert.AlgRSA2048,
			PublicKey:   rsa2048(gemRCA2ModulusHex, rsaCommonExponent),
			SubjectDN:   "C=DE, O=gematik GmbH, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA2",
			NotBefore:   time.Date(2016, 12, 9, 8, 41, 56, 0, time.UTC),
			NotAfter:    time.Date(2026, 12, 7, 8, 41, 56, 0, time.UTC),
			Source:      "https://download.tsl.ti-dienste.de/ROOT-CA/GEM.RCA2.der",
			Fingerprint: "848fda162c607b492c62f625840e6451285c40c7334ec8dd659d093236ebc9ec",
			Notes:       "Legacy production root, RSA-2048 SHA-256. Phasing out; still trusted by older issued cards.",
		},
		{
			Name:        "gematik.RCA6",
			KeyAlg:      cvcert.AlgRSA2048,
			PublicKey:   rsa2048(gemRCA6ModulusHex, rsaCommonExponent),
			SubjectDN:   "C=DE, O=gematik GmbH, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA6",
			NotBefore:   time.Date(2021, 11, 11, 8, 50, 44, 0, time.UTC),
			NotAfter:    time.Date(2031, 11, 9, 8, 50, 44, 0, time.UTC),
			Source:      "https://download.tsl.ti-dienste.de/ROOT-CA/GEM.RCA6.der",
			Fingerprint: "7c250199c7d87058a3a8f84f2a3c7727a27511670dac596535273af0452d84f3",
			Notes:       "Currently active production root for the TI X.509 hierarchy.",
		},
		{
			Name:        "gematik.RCA9",
			KeyAlg:      cvcert.AlgRSA2048,
			PublicKey:   rsa2048(gemRCA9ModulusHex, rsaCommonExponent),
			SubjectDN:   "C=DE, O=gematik GmbH, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA9",
			NotBefore:   time.Date(2025, 6, 4, 8, 27, 44, 0, time.UTC),
			NotAfter:    time.Date(2035, 6, 2, 8, 27, 44, 0, time.UTC),
			Source:      "https://download.tsl.ti-dienste.de/ROOT-CA/GEM.RCA9.der",
			Fingerprint: "b7eee57557c31d43263d5e6cfe98185acf2b7d338c2261a054368d5dd5432442",
			Notes:       "Next-generation production root, RSA-2048 SHA-256. Successor to RCA6.",
		},
		// TODO(c2c-cvc-root): gematik CVC-Root (production). Signs the
		// CV-certs presented by eGK / SMC-B during the gemSpec_COS §13
		// card-to-card handshake. NOT published via the TSL endpoint
		// above; see gemSpec_PKI and gemSpec_CVC_Root. Typically
		// Brainpool P-256/P-384. Add as a Root with KeyAlg =
		// AlgBrainpoolP256r1 or P384r1 once located.
	}
}

// TestRoots returns gematik's TEST-ONLY (NOT-VALID) root trust anchors.
// Usable only against test cards / test back-ends. Real eGK / SMC-B /
// HBA cards reject any chain rooted in a TEST-ONLY CA.
func TestRoots() []Root {
	return []Root{
		{
			Name:        "gematik.RCA2.TEST-ONLY",
			KeyAlg:      cvcert.AlgRSA2048,
			PublicKey:   rsa2048(gemRCA2TestModulusHex, rsaCommonExponent),
			SubjectDN:   "C=DE, O=gematik GmbH NOT-VALID, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA2 TEST-ONLY",
			NotBefore:   time.Date(2016, 11, 17, 15, 50, 57, 0, time.UTC),
			NotAfter:    time.Date(2026, 11, 15, 15, 50, 57, 0, time.UTC),
			Source:      "https://download-test.tsl.ti-dienste.de/ROOT-CA/GEM.RCA2_TEST-ONLY.der",
			Fingerprint: "074609b1d76a19286efcb90634a0d6aea36826ee1ffc52c696235b7f4a87872d",
			Notes:       "Anchors the slot-2 test SMC-B in this project (issuer GEM.SMCB-CA24 TEST-ONLY).",
		},
		{
			Name:        "gematik.RCA6.TEST-ONLY",
			KeyAlg:      cvcert.AlgRSA2048,
			PublicKey:   rsa2048(gemRCA6TestModulusHex, rsaCommonExponent),
			SubjectDN:   "C=DE, O=gematik GmbH NOT-VALID, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA6 TEST-ONLY",
			NotBefore:   time.Date(2021, 10, 28, 7, 24, 14, 0, time.UTC),
			NotAfter:    time.Date(2031, 10, 26, 7, 24, 14, 0, time.UTC),
			Source:      "https://download-test.tsl.ti-dienste.de/ROOT-CA/GEM.RCA6_TEST-ONLY.der",
			Fingerprint: "3cff0528cf0ff06e5a99f157afad505bca9dfa012861a471f71ca98cea5721ed",
			Notes:       "Current test-PKI root corresponding to production RCA6.",
		},
		{
			Name:        "gematik.RCA9.TEST-ONLY",
			KeyAlg:      cvcert.AlgRSA2048,
			PublicKey:   rsa2048(gemRCA9TestModulusHex, rsaCommonExponent),
			SubjectDN:   "C=DE, O=gematik GmbH NOT-VALID, OU=Zentrale Root-CA der Telematikinfrastruktur, CN=GEM.RCA9 TEST-ONLY",
			NotBefore:   time.Date(2025, 5, 8, 12, 1, 40, 0, time.UTC),
			NotAfter:    time.Date(2035, 5, 6, 12, 1, 40, 0, time.UTC),
			Source:      "https://download-test.tsl.ti-dienste.de/ROOT-CA/GEM.RCA9_TEST-ONLY.der",
			Fingerprint: "75bb81da87f1841dc667868149df04e469c2006e1da0d27eb1c46d0a234c55bd",
			Notes:       "Next-generation test-PKI root corresponding to production RCA9.",
		},
		// TODO(c2c-cvc-root): gematik CVC-Root TEST-ONLY (Brainpool ECC).
		// Required for live C2C against a test eGK; not needed for the
		// X.509 chain validation of the slot-2 SMC-B.
	}
}

// AllRoots returns ProductionRoots ++ TestRoots. Convenience for tools
// that want to display whichever root applies for an arbitrary cert.
func AllRoots() []Root {
	p := ProductionRoots()
	t := TestRoots()
	out := make([]Root, 0, len(p)+len(t))
	out = append(out, p...)
	out = append(out, t...)
	return out
}
