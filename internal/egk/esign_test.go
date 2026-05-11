package egk

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"math/big"
	"testing"
	"time"
)

func TestDERSequenceTotalLen(t *testing.T) {
	cases := []struct {
		in   []byte
		want uint32
		ok   bool
	}{
		{[]byte{0x30, 0x05}, 7, true},                   // short form, 5 + 2
		{[]byte{0x30, 0x7F, 0, 0}, 0x7F + 2, true},      // boundary short form
		{[]byte{0x30, 0x81, 0xAA}, 0xAA + 3, true},      // long form 0x81
		{[]byte{0x30, 0x82, 0x01, 0x10}, 0x110 + 4, true}, // long form 0x82
		{[]byte{0x31, 0x05}, 0, false},                  // not a SEQUENCE
		{[]byte{}, 0, false},
		{[]byte{0x30}, 0, false},        // too short
		{[]byte{0x30, 0x81}, 0, false},  // short 0x81 header
		{[]byte{0x30, 0x82, 0x01}, 0, false},
		{[]byte{0x30, 0x83, 0, 0, 0, 0}, 0, false}, // 0x83 long form not supported
	}
	for _, c := range cases {
		got, err := derSequenceTotalLen(c.in)
		if c.ok && err != nil {
			t.Errorf("derSequenceTotalLen(%X) errored: %v", c.in, err)
		}
		if !c.ok && err == nil {
			t.Errorf("derSequenceTotalLen(%X) should have errored", c.in)
		}
		if c.ok && got != c.want {
			t.Errorf("derSequenceTotalLen(%X) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestReadCertEF_RSA(t *testing.T) {
	// Generate a tiny RSA cert in-memory so the test runs without a card.
	der := makeRSACert(t, "Test Holder", "Test CA")
	cert := scriptCertEF(t, 0xC500, der)
	c, err := readCertEF(cert, 0xC500)
	if err != nil {
		t.Fatal(err)
	}
	if c.Certificate == nil {
		t.Fatal("expected crypto/x509 to parse RSA cert")
	}
	if c.Certificate.Subject.CommonName != "Test Holder" {
		t.Errorf("subject CN = %q", c.Certificate.Subject.CommonName)
	}
	if !bytes.Equal(c.DER, der) {
		t.Errorf("DER mismatch")
	}
}

func TestReadCertEF_PartialParseFallback(t *testing.T) {
	// Generate an ECDSA cert on P-256. crypto/x509 accepts NIST P-256, so we
	// fake the brainpool path by mangling the OID inside the cert.
	der := makeECDSACert(t, "Brainpool Holder", "Brainpool CA")
	// Walk to the ECDSA public-key OID (1.2.840.10045.2.1 = id-ecPublicKey)
	// and the curve parameter OID, swap the curve OID for an unrecognised one.
	// The simplest way is to flip a byte in any OID after a certain offset
	// so x509.ParseCertificate refuses but the structure stays parseable.
	bad := append([]byte{}, der...)
	// Find OID 1.2.840.10045.3.1.7 (prime256v1) and corrupt it.
	idx := bytes.Index(bad, []byte{0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x03, 0x01, 0x07})
	if idx < 0 {
		t.Skip("could not locate curve OID in synthetic ECDSA cert")
	}
	bad[idx+7] = 0x99 // flip last byte → unknown curve OID
	c, err := readCertEF(scriptCertEF(t, 0xC504, bad), 0xC504)
	if err != nil {
		t.Fatalf("partial parse should succeed: %v", err)
	}
	if c.Certificate != nil {
		t.Error("expected crypto/x509 to reject corrupted curve")
	}
	if c.Subject.CommonName != "Brainpool Holder" {
		t.Errorf("subject CN = %q", c.Subject.CommonName)
	}
	// x509.CreateCertificate(tmpl, tmpl) self-signs → issuer.CN inherits the
	// subject CN. We just need to confirm the field is non-empty (proves
	// the partial parser walked the Issuer SEQUENCE).
	if c.Issuer.CommonName == "" {
		t.Errorf("issuer CN unexpectedly empty")
	}
	if c.NotAfter.Year() < 2024 {
		t.Errorf("NotAfter year too small: %v", c.NotAfter)
	}
}

func TestReadCertEF_SelectFails(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C02C500", sw: 0x6A82},
		},
	}
	if _, err := readCertEF(card, 0xC500); err == nil {
		t.Error("expected error when SELECT fails")
	}
}

func TestReadCertEF_NotADERSequence(t *testing.T) {
	// SELECT ok, but the first 4 bytes don't start with 30 (SEQUENCE).
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C02C500", sw: 0x9000},
			{wantAPDU: "00B0000004", respData: []byte{0x00, 0x00, 0x00, 0x00}, sw: 0x9000},
		},
	}
	if _, err := readCertEF(card, 0xC500); err == nil {
		t.Error("expected error on non-DER content")
	}
}

func TestReadESIGN_SelectFails(t *testing.T) {
	// SELECT DF.ESIGN fails both with FCP and without.
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{sw: 0x6A82},
			{sw: 0x6A82},
		},
	}
	if _, err := readESIGN(card); err == nil {
		t.Error("expected error when DF.ESIGN cannot be selected")
	}
}

// ----- test helpers -----

// scriptCertEF returns a fakeCard whose script answers SELECT EF(fid) +
// the chunked READ BINARY sequence with the given DER bytes.
func scriptCertEF(t *testing.T, fid uint16, der []byte) *fakeCard {
	script := []scriptEntry{
		{wantAPDU: selectEFWant(fid), sw: 0x9000},
		{wantAPDU: "00B0000004", respData: der[:4], sw: 0x9000},
	}
	offset := uint16(4)
	for int(offset) < len(der) {
		remaining := len(der) - int(offset)
		n := int(readChunkSize)
		if remaining < n {
			n = remaining
		}
		want := bytes.NewBuffer([]byte{claISO, insReadBinary, byte(offset >> 8), byte(offset & 0xFF), byte(n)})
		script = append(script, scriptEntry{
			wantAPDU: hexOf(want.Bytes()),
			respData: der[offset : int(offset)+n],
			sw:       0x9000,
		})
		offset += uint16(n)
	}
	return &fakeCard{t: t, script: script}
}

func selectEFWant(fid uint16) string {
	return hexOf([]byte{claISO, insSelect, p1SelectEF, p2NoFCI, 0x02, byte(fid >> 8), byte(fid & 0xFF)})
}

func hexOf(b []byte) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 2*len(b))
	for i, x := range b {
		out[2*i] = hex[x>>4]
		out[2*i+1] = hex[x&0x0F]
	}
	return string(out)
}

func makeRSACert(t *testing.T, subjectCN, issuerCN string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: subjectCN},
		Issuer:       pkix.Name{CommonName: issuerCN},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour * 24 * 365),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func makeECDSACert(t *testing.T, subjectCN, issuerCN string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: subjectCN},
		Issuer:       pkix.Name{CommonName: issuerCN},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour * 24 * 365),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

var _ = binary.BigEndian // keep import used in case future tests need it
