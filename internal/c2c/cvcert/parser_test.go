package cvcert

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"strings"
	"testing"
	"time"
)

// --- TLV builders used to synthesize test fixtures. ---------------------

// encodeTag re-encodes a tag stored as a big-endian uint32 back to its
// on-the-wire byte form.
func encodeTag(tag uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], tag)
	// Strip leading 0x00 bytes.
	for i, b := range buf {
		if b != 0 {
			return buf[i:]
		}
	}
	return []byte{0}
}

func encodeLength(n int) []byte {
	switch {
	case n < 0x80:
		return []byte{byte(n)}
	case n < 0x100:
		return []byte{0x81, byte(n)}
	case n < 0x10000:
		return []byte{0x82, byte(n >> 8), byte(n)}
	default:
		return []byte{0x83, byte(n >> 16), byte(n >> 8), byte(n)}
	}
}

func tlv(tag uint32, value []byte) []byte {
	t := encodeTag(tag)
	l := encodeLength(len(value))
	out := make([]byte, 0, len(t)+len(l)+len(value))
	out = append(out, t...)
	out = append(out, l...)
	out = append(out, value...)
	return out
}

// --- Synthesized inputs --------------------------------------------------

func bcdDate(yy, mm, dd int) []byte {
	return []byte{byte(yy / 10), byte(yy % 10), byte(mm / 10), byte(mm % 10), byte(dd / 10), byte(dd % 10)}
}

func asciiDate(yy, mm, dd int) []byte {
	return []byte{
		byte('0' + yy/10), byte('0' + yy%10),
		byte('0' + mm/10), byte('0' + mm%10),
		byte('0' + dd/10), byte('0' + dd%10),
	}
}

func buildRSABody(t *testing.T) []byte {
	t.Helper()
	// A real RSA-2048 modulus would be 256 bytes; use 256 dummy bytes so
	// the length encoding exercises the 0x82 long form too.
	modulus := bytes.Repeat([]byte{0xAB}, 256)
	modulus[0] = 0xC1 // ensure high bit set / non-zero leading byte
	exponent := []byte{0x01, 0x00, 0x01}

	pubKey := bytes.Join([][]byte{
		tlv(tagOID, oidRSAv15SHA256),
		tlv(tagRSAModulus, modulus),
		tlv(tagRSAExp, exponent),
	}, nil)

	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70}),
		tlv(tagCAR, []byte("DECA00001")),
		tlv(tagPubKey, pubKey),
		tlv(tagCHR, []byte("DEHCA00012345")),
		tlv(tagCHAT, []byte{0x12, 0x34, 0x56}),
		tlv(tagEffective, bcdDate(24, 5, 1)),
		tlv(tagExpiration, bcdDate(29, 4, 30)),
	}, nil)
	return tlv(tagCertBody, body)
}

func buildRSACert(t *testing.T) []byte {
	t.Helper()
	body := buildRSABody(t)
	sig := bytes.Repeat([]byte{0x55}, 256)
	return tlv(tagCVCert, append(append([]byte(nil), body...), tlv(tagSignature, sig)...))
}

func buildECCBody(t *testing.T) []byte {
	t.Helper()
	// 32-byte X and Y for P-256 (values arbitrary).
	x := bytes.Repeat([]byte{0x01}, 32)
	y := bytes.Repeat([]byte{0x02}, 32)
	pt := append([]byte{0x04}, append(x, y...)...)

	pubKey := bytes.Join([][]byte{
		tlv(tagOID, oidECDSAbp256),
		tlv(tagECPoint, pt),
	}, nil)

	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70}),
		tlv(tagCAR, []byte("DECA00002")),
		tlv(tagPubKey, pubKey),
		tlv(tagCHR, []byte("DEHCA00099999")),
		tlv(tagEffective, asciiDate(25, 1, 15)),
		tlv(tagExpiration, asciiDate(30, 1, 14)),
	}, nil)
	return tlv(tagCertBody, body)
}

func buildECCCert(t *testing.T) []byte {
	t.Helper()
	body := buildECCBody(t)
	sig := bytes.Repeat([]byte{0xAA}, 64) // r||s for P-256
	return tlv(tagCVCert, append(append([]byte(nil), body...), tlv(tagSignature, sig)...))
}

// --- Tests --------------------------------------------------------------

func TestParseRSACert(t *testing.T) {
	in := buildRSACert(t)
	c, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.CPI != 0x70 {
		t.Errorf("CPI = 0x%02X, want 0x70", c.CPI)
	}
	if c.CAR != "DECA00001" {
		t.Errorf("CAR = %q", c.CAR)
	}
	if c.CHR != "DEHCA00012345" {
		t.Errorf("CHR = %q", c.CHR)
	}
	if c.KeyAlg != AlgRSA2048 {
		t.Errorf("KeyAlg = %v, want %v", c.KeyAlg, AlgRSA2048)
	}
	rsa, ok := c.PublicKey.(*RSAPublicKey)
	if !ok {
		t.Fatalf("PublicKey type = %T, want *RSAPublicKey", c.PublicKey)
	}
	if rsa.E.Cmp(big.NewInt(65537)) != 0 {
		t.Errorf("E = %s, want 65537", rsa.E)
	}
	if rsa.N.BitLen() < 2040 {
		t.Errorf("N bitlen = %d, want >= 2040", rsa.N.BitLen())
	}
	want := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	if !c.NotBefore.Equal(want) {
		t.Errorf("NotBefore = %v, want %v", c.NotBefore, want)
	}
	want = time.Date(2029, 4, 30, 0, 0, 0, 0, time.UTC)
	if !c.NotAfter.Equal(want) {
		t.Errorf("NotAfter = %v, want %v", c.NotAfter, want)
	}
	if len(c.CHAT) != 3 {
		t.Errorf("CHAT len = %d, want 3", len(c.CHAT))
	}
	if len(c.Signature) != 256 {
		t.Errorf("Signature len = %d, want 256", len(c.Signature))
	}

	// Raw must alias into input and re-encode the entire wrapper.
	if !bytes.Equal(c.Raw, in) {
		t.Error("Raw does not equal input")
	}

	// Body must start with the 7F4E tag.
	if !bytes.HasPrefix(c.Body, []byte{0x7F, 0x4E}) {
		t.Errorf("Body does not start with 7F4E: %X", c.Body[:4])
	}

	// Body must be a subslice of Raw.
	if !bytes.Contains(in, c.Body) {
		t.Error("Body not found inside Raw")
	}
}

func TestParseECCCert(t *testing.T) {
	in := buildECCCert(t)
	c, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.KeyAlg != AlgBrainpoolP256r1 {
		t.Errorf("KeyAlg = %v", c.KeyAlg)
	}
	ecc, ok := c.PublicKey.(*ECCPublicKey)
	if !ok {
		t.Fatalf("PublicKey type = %T", c.PublicKey)
	}
	if ecc.Curve != AlgBrainpoolP256r1 {
		t.Errorf("Curve = %v", ecc.Curve)
	}
	wantX := new(big.Int).SetBytes(bytes.Repeat([]byte{0x01}, 32))
	if ecc.X.Cmp(wantX) != 0 {
		t.Errorf("X mismatch")
	}
	wantY := new(big.Int).SetBytes(bytes.Repeat([]byte{0x02}, 32))
	if ecc.Y.Cmp(wantY) != 0 {
		t.Errorf("Y mismatch")
	}
	if c.CHAT != nil {
		t.Errorf("CHAT = %X, want nil (omitted in this fixture)", c.CHAT)
	}
	if len(c.Signature) != 64 {
		t.Errorf("Signature len = %d, want 64", len(c.Signature))
	}
	want := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	if !c.NotBefore.Equal(want) {
		t.Errorf("NotBefore = %v", c.NotBefore)
	}
}

func TestParseBareBody(t *testing.T) {
	body := buildECCBody(t)
	c, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Raw != nil {
		t.Error("Raw should be nil when input is a bare 7F4E body")
	}
	if c.Signature != nil {
		t.Error("Signature should be nil when input is a bare 7F4E body")
	}
	if c.KeyAlg != AlgBrainpoolP256r1 {
		t.Errorf("KeyAlg = %v", c.KeyAlg)
	}
}

func TestParseBodyFunc(t *testing.T) {
	body := buildRSABody(t)
	c, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody: %v", err)
	}
	if c.KeyAlg != AlgRSA2048 {
		t.Errorf("KeyAlg = %v", c.KeyAlg)
	}
	if !bytes.Equal(c.Body, body) {
		t.Error("Body != input")
	}
}

func TestParseBodyRejectsWrapper(t *testing.T) {
	in := buildRSACert(t)
	_, err := ParseBody(in)
	if err == nil {
		t.Fatal("ParseBody accepted a 7F21 wrapper")
	}
	if !strings.Contains(err.Error(), "7F4E") {
		t.Errorf("error = %v, want mention of 7F4E", err)
	}
}

// --- Negative cases -----------------------------------------------------

func TestEmptyInput(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Error("Parse(nil) succeeded")
	}
	if _, err := ParseBody(nil); err == nil {
		t.Error("ParseBody(nil) succeeded")
	}
}

func TestTruncatedWrapper(t *testing.T) {
	in := buildRSACert(t)
	// Drop the last 10 bytes of the signature.
	_, err := Parse(in[:len(in)-10])
	if err == nil {
		t.Error("Parse accepted truncated input")
	}
}

func TestWrongOuterTag(t *testing.T) {
	in := tlv(0x30, []byte{0x01, 0x02}) // SEQUENCE — not a CV-cert tag
	_, err := Parse(in)
	if err == nil {
		t.Fatal("Parse accepted wrong outer tag")
	}
	if !strings.Contains(err.Error(), "unexpected outer tag") {
		t.Errorf("error = %v", err)
	}
}

func TestMissingRequiredField(t *testing.T) {
	// Build a body missing the CHR (5F20).
	x := bytes.Repeat([]byte{0x01}, 32)
	y := bytes.Repeat([]byte{0x02}, 32)
	pt := append([]byte{0x04}, append(x, y...)...)
	pubKey := bytes.Join([][]byte{
		tlv(tagOID, oidECDSAbp256),
		tlv(tagECPoint, pt),
	}, nil)
	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70}),
		tlv(tagCAR, []byte("DECA00001")),
		tlv(tagPubKey, pubKey),
		// CHR missing
		tlv(tagEffective, bcdDate(24, 1, 1)),
		tlv(tagExpiration, bcdDate(29, 1, 1)),
	}, nil)
	bodyTLV := tlv(tagCertBody, body)
	_, err := ParseBody(bodyTLV)
	if err == nil {
		t.Fatal("ParseBody accepted body missing CHR")
	}
	if !strings.Contains(err.Error(), "CHR") {
		t.Errorf("error = %v, want mention of CHR", err)
	}
}

func TestBadBCDDate(t *testing.T) {
	// Build a body with a BCD date containing nibbles > 9 (non-ASCII so we
	// fall into the BCD branch).
	bad := []byte{0x0A, 0x05, 0x00, 0x01, 0x00, 0x01}
	pubKey := bytes.Join([][]byte{
		tlv(tagOID, oidECDSAbp256),
		tlv(tagECPoint, append([]byte{0x04}, bytes.Repeat([]byte{0x00}, 64)...)),
	}, nil)
	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70}),
		tlv(tagCAR, []byte("X")),
		tlv(tagPubKey, pubKey),
		tlv(tagCHR, []byte("Y")),
		tlv(tagEffective, bad),
		tlv(tagExpiration, bcdDate(29, 1, 1)),
	}, nil)
	_, err := ParseBody(tlv(tagCertBody, body))
	if err == nil {
		t.Fatal("ParseBody accepted invalid BCD date")
	}
	if !strings.Contains(err.Error(), "effective") && !strings.Contains(err.Error(), "BCD") {
		t.Errorf("error = %v", err)
	}
}

func TestUnknownAlgorithmOID(t *testing.T) {
	unknownOID := []byte{0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x03, 0x01, 0x07} // P-256 (X9.62)
	pubKey := bytes.Join([][]byte{
		tlv(tagOID, unknownOID),
		tlv(tagECPoint, append([]byte{0x04}, bytes.Repeat([]byte{0x00}, 64)...)),
	}, nil)
	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70}),
		tlv(tagCAR, []byte("X")),
		tlv(tagPubKey, pubKey),
		tlv(tagCHR, []byte("Y")),
		tlv(tagEffective, bcdDate(24, 1, 1)),
		tlv(tagExpiration, bcdDate(29, 1, 1)),
	}, nil)
	_, err := ParseBody(tlv(tagCertBody, body))
	if err == nil {
		t.Fatal("ParseBody accepted unknown algorithm OID")
	}
	if !strings.Contains(err.Error(), "unknown public-key OID") {
		t.Errorf("error = %v", err)
	}
}

func TestECCPointWrongLength(t *testing.T) {
	// Brainpool P256 expects 65 bytes; supply 33.
	pt := append([]byte{0x04}, bytes.Repeat([]byte{0x00}, 32)...)
	pubKey := bytes.Join([][]byte{
		tlv(tagOID, oidECDSAbp256),
		tlv(tagECPoint, pt),
	}, nil)
	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70}),
		tlv(tagCAR, []byte("X")),
		tlv(tagPubKey, pubKey),
		tlv(tagCHR, []byte("Y")),
		tlv(tagEffective, bcdDate(24, 1, 1)),
		tlv(tagExpiration, bcdDate(29, 1, 1)),
	}, nil)
	_, err := ParseBody(tlv(tagCertBody, body))
	if err == nil {
		t.Fatal("ParseBody accepted wrong-length ECC point")
	}
}

func TestECCPointNotUncompressed(t *testing.T) {
	pt := append([]byte{0x02}, bytes.Repeat([]byte{0x00}, 64)...)
	pubKey := bytes.Join([][]byte{
		tlv(tagOID, oidECDSAbp256),
		tlv(tagECPoint, pt),
	}, nil)
	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70}),
		tlv(tagCAR, []byte("X")),
		tlv(tagPubKey, pubKey),
		tlv(tagCHR, []byte("Y")),
		tlv(tagEffective, bcdDate(24, 1, 1)),
		tlv(tagExpiration, bcdDate(29, 1, 1)),
	}, nil)
	_, err := ParseBody(tlv(tagCertBody, body))
	if err == nil {
		t.Fatal("ParseBody accepted compressed ECC point")
	}
}

func TestRSAMissingExponent(t *testing.T) {
	modulus := bytes.Repeat([]byte{0xAB}, 256)
	pubKey := bytes.Join([][]byte{
		tlv(tagOID, oidRSAv15SHA256),
		tlv(tagRSAModulus, modulus),
		// no exponent
	}, nil)
	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70}),
		tlv(tagCAR, []byte("X")),
		tlv(tagPubKey, pubKey),
		tlv(tagCHR, []byte("Y")),
		tlv(tagEffective, bcdDate(24, 1, 1)),
		tlv(tagExpiration, bcdDate(29, 1, 1)),
	}, nil)
	_, err := ParseBody(tlv(tagCertBody, body))
	if err == nil {
		t.Fatal("ParseBody accepted RSA key without exponent")
	}
}

func TestTrailingBytes(t *testing.T) {
	in := append(buildRSACert(t), 0xDE, 0xAD)
	_, err := Parse(in)
	if err == nil {
		t.Error("Parse accepted trailing bytes")
	}
}

func TestDuplicateBodyTag(t *testing.T) {
	body := buildECCBody(t)
	// Insert an extra CPI inside the body.
	// Decode, append, re-encode.
	tag, contents, _, err := readTLV(body)
	if err != nil || tag != tagCertBody {
		t.Fatalf("setup failed: %v", err)
	}
	doubled := append(append([]byte(nil), contents...), tlv(tagCPI, []byte{0x70})...)
	bad := tlv(tagCertBody, doubled)
	_, err = ParseBody(bad)
	if err == nil {
		t.Fatal("ParseBody accepted duplicate CPI tag")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error = %v", err)
	}
}

func TestMissingSignature(t *testing.T) {
	body := buildRSABody(t)
	wrapper := tlv(tagCVCert, body) // 7F21 with only a body, no signature
	_, err := Parse(wrapper)
	if err == nil {
		t.Fatal("Parse accepted wrapper without signature")
	}
}

func TestWrongLengthCPI(t *testing.T) {
	pubKey := bytes.Join([][]byte{
		tlv(tagOID, oidECDSAbp256),
		tlv(tagECPoint, append([]byte{0x04}, bytes.Repeat([]byte{0x00}, 64)...)),
	}, nil)
	body := bytes.Join([][]byte{
		tlv(tagCPI, []byte{0x70, 0x71}), // length 2 instead of 1
		tlv(tagCAR, []byte("X")),
		tlv(tagPubKey, pubKey),
		tlv(tagCHR, []byte("Y")),
		tlv(tagEffective, bcdDate(24, 1, 1)),
		tlv(tagExpiration, bcdDate(29, 1, 1)),
	}, nil)
	_, err := ParseBody(tlv(tagCertBody, body))
	if err == nil {
		t.Fatal("ParseBody accepted CPI with wrong length")
	}
}

// --- Round-trip / Raw/Body slicing --------------------------------------

func TestRawAndBodySliceAsExpected(t *testing.T) {
	in := buildECCCert(t)
	c, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	// c.Raw should equal in.
	if !bytes.Equal(c.Raw, in) {
		t.Error("Raw != input")
	}
	// c.Body must appear inside c.Raw and start at the 7F4E offset.
	idx := bytes.Index(in, []byte{0x7F, 0x4E})
	if idx < 0 {
		t.Fatal("could not locate 7F4E in input")
	}
	// Body length is recoverable from c.Body.
	if !bytes.Equal(in[idx:idx+len(c.Body)], c.Body) {
		t.Error("Body bytes do not match the body span of Raw")
	}
}

// --- Helpers tests ------------------------------------------------------

func TestReadLengthForms(t *testing.T) {
	cases := []struct {
		in   []byte
		want int
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x7F}, 0x7F},
		{[]byte{0x81, 0x80}, 0x80},
		{[]byte{0x82, 0x01, 0x00}, 256},
	}
	for _, tc := range cases {
		v, _, err := readLength(tc.in)
		if err != nil {
			t.Errorf("readLength(%X) err: %v", tc.in, err)
			continue
		}
		if v != tc.want {
			t.Errorf("readLength(%X) = %d, want %d", tc.in, v, tc.want)
		}
	}
	// Indefinite form must be rejected.
	if _, _, err := readLength([]byte{0x80}); err == nil {
		t.Error("readLength accepted indefinite form")
	}
}

func TestReadTag(t *testing.T) {
	// Single-byte tag.
	tag, n, err := readTag([]byte{0x42, 0xFF})
	if err != nil || n != 1 || tag != 0x42 {
		t.Errorf("readTag single: tag=%X n=%d err=%v", tag, n, err)
	}
	// Two-byte tag (7F49).
	tag, n, err = readTag([]byte{0x7F, 0x49, 0xFF})
	if err != nil || n != 2 || tag != 0x7F49 {
		t.Errorf("readTag two-byte: tag=%X n=%d err=%v", tag, n, err)
	}
	// Truncated multi-byte tag.
	if _, _, err := readTag([]byte{0x7F}); err == nil {
		t.Error("readTag accepted truncated multi-byte tag")
	}
}
