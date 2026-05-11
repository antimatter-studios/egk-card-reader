package cvcert

// BER-TLV tags used in a CV-certificate per BSI TR-03110-3 §C.1 and
// gemSpec_PKI. All tags are encoded big-endian as raw uint32 so we can
// compare them directly against the value returned by readTag.
const (
	tagCVCert     uint32 = 0x7F21 // CV Certificate (outer wrapper)
	tagCertBody   uint32 = 0x7F4E // Certificate Body (signed-over region)
	tagCPI        uint32 = 0x5F29 // Certificate Profile Indicator
	tagCAR        uint32 = 0x42   // Certificate Authority Reference (issuer)
	tagPubKey     uint32 = 0x7F49 // Public Key
	tagOID        uint32 = 0x06   // OID identifying the public-key algorithm
	tagRSAModulus uint32 = 0x81   // RSA modulus n
	tagRSAExp     uint32 = 0x82   // RSA public exponent e
	tagECPoint    uint32 = 0x86   // ECC public point (uncompressed 04||X||Y)
	tagCHR        uint32 = 0x5F20 // Certificate Holder Reference (subject)
	tagCHAT       uint32 = 0x7F4C // Certificate Holder Authorisation Template
	tagEffective  uint32 = 0x5F25 // Effective Date (6 byte BCD YYMMDD)
	tagExpiration uint32 = 0x5F24 // Expiration Date  (6 byte BCD YYMMDD)
	tagSignature  uint32 = 0x5F37 // Signature value
)

// Public-key algorithm OIDs per gemSpec_PKI.
//
// Encoded as the dotted form converts to DER bytes after the standard
// {1 3, 0 4, ...} packing rules; we keep the canonical DER bytes here so
// we can compare against the value of an OID TLV directly without doing
// repeated re-encoding work.
//
// 0.4.0.127.0.7.2.2.2.1.2  — RSA-2048 PKCS#1 v1.5 + SHA-256 (ta-rsa-v1-5-SHA-256)
// 0.4.0.127.0.7.1.1.4.1.2  — ECDSA + Brainpool P256r1 + SHA-256
// 0.4.0.127.0.7.1.1.4.1.3  — ECDSA + Brainpool P384r1 + SHA-384  (TODO(spec): verify)
// 0.4.0.127.0.7.1.1.4.1.4  — ECDSA + Brainpool P512r1 + SHA-512  (TODO(spec): verify)
var (
	oidRSAv15SHA256 = []byte{0x04, 0x00, 0x7F, 0x00, 0x07, 0x02, 0x02, 0x02, 0x01, 0x02}
	oidECDSAbp256   = []byte{0x04, 0x00, 0x7F, 0x00, 0x07, 0x01, 0x01, 0x04, 0x01, 0x02}
	oidECDSAbp384   = []byte{0x04, 0x00, 0x7F, 0x00, 0x07, 0x01, 0x01, 0x04, 0x01, 0x03}
	oidECDSAbp512   = []byte{0x04, 0x00, 0x7F, 0x00, 0x07, 0x01, 0x01, 0x04, 0x01, 0x04}
)
