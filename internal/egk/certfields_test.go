package egk

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"strings"
	"testing"
	"time"
)

// brainpoolP256r1WithSHA256 — gematik signature OID that crypto/x509 doesn't
// recognise, used by ECDSA certs in DF.ESIGN (C504). Synthetic value used
// only to exercise the SigAlgOID fallback branch in certFields.
var brainpoolP256r1WithSHA256 = asn1.ObjectIdentifier{1, 3, 36, 3, 3, 2, 8, 1, 1, 7}

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return tt
}

// rsaCertFixture builds a Cert as if crypto/x509 had parsed an RSA cert
// successfully — Certificate is non-nil, SignatureAlgorithm carries through.
func rsaCertFixture(t *testing.T) *Cert {
	t.Helper()
	return &Cert{
		FID: 0xC500,
		Certificate: &x509.Certificate{
			SignatureAlgorithm: x509.SHA256WithRSA,
		},
		Subject:   pkix.Name{CommonName: "TEST-CARDHOLDER, Demo"},
		Issuer:    pkix.Name{CommonName: "GEM.HBA-CA TEST-ONLY"},
		NotBefore: mustParseDate(t, "2023-01-01"),
		NotAfter:  mustParseDate(t, "2028-12-31"),
	}
}

// ecdsaPartialCertFixture mirrors the brainpool case — crypto/x509 rejected
// the cert so Certificate is nil and only the partial-parse fields are set.
func ecdsaPartialCertFixture(t *testing.T) *Cert {
	t.Helper()
	return &Cert{
		FID:       0xC504,
		Subject:   pkix.Name{CommonName: "TEST-CARDHOLDER, Demo"},
		Issuer:    pkix.Name{CommonName: "GEM.HBA-CA TEST-ONLY"},
		NotBefore: mustParseDate(t, "2023-02-01"),
		NotAfter:  mustParseDate(t, "2028-01-31"),
		SigAlgOID: brainpoolP256r1WithSHA256,
	}
}

func TestCertFields_Nil(t *testing.T) {
	if got := certFields(nil); got != nil {
		t.Errorf("certFields(nil) = %v, want nil", got)
	}
}

func TestCertFields_RSAFullyParsed(t *testing.T) {
	c := rsaCertFixture(t)
	fields := certFields(c)
	if len(fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(fields))
	}
	checks := map[string]string{
		"Cert C500 Subject":     "TEST-CARDHOLDER, Demo",
		"Cert C500 Issuer":      "GEM.HBA-CA TEST-ONLY",
		"Cert C500 Valid From":  "2023-01-01",
		"Cert C500 Valid Until": "2028-12-31",
		"Cert C500 Algorithm":   x509.SHA256WithRSA.String(),
	}
	for label, want := range checks {
		f, ok := fieldByLabel(fields, label)
		if !ok {
			t.Errorf("missing field %q", label)
			continue
		}
		if f.Value != want {
			t.Errorf("%s: got %q, want %q", label, f.Value, want)
		}
		if f.Source != "DF.ESIGN.EF.C(C500)" {
			t.Errorf("%s source = %q, want DF.ESIGN.EF.C(C500)", label, f.Source)
		}
		if f.Note == "" {
			t.Errorf("%s note should be non-empty", label)
		}
	}
	algo, _ := fieldByLabel(fields, "Cert C500 Algorithm")
	if strings.Contains(algo.Note, "OID surfaced") {
		t.Errorf("RSA algo note should not mention OID fallback, got %q", algo.Note)
	}
}

func TestCertFields_ECDSAPartialBrainpool(t *testing.T) {
	c := ecdsaPartialCertFixture(t)
	fields := certFields(c)
	if len(fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(fields))
	}
	algo, ok := fieldByLabel(fields, "Cert C504 Algorithm")
	if !ok {
		t.Fatal("Algorithm field missing")
	}
	if algo.Value != brainpoolP256r1WithSHA256.String() {
		t.Errorf("algo value = %q, want %q", algo.Value, brainpoolP256r1WithSHA256.String())
	}
	if !strings.Contains(algo.Note, "OID surfaced") {
		t.Errorf("algo note should mention OID fallback, got %q", algo.Note)
	}
	if !strings.Contains(algo.Note, "brainpoolP256r1") {
		t.Errorf("algo note should mention brainpool curve, got %q", algo.Note)
	}
	subj, _ := fieldByLabel(fields, "Cert C504 Subject")
	if subj.Source != "DF.ESIGN.EF.C(C504)" {
		t.Errorf("Source = %q, want DF.ESIGN.EF.C(C504)", subj.Source)
	}
	if subj.Value != "TEST-CARDHOLDER, Demo" {
		t.Errorf("Subject CN = %q", subj.Value)
	}
	validFrom, _ := fieldByLabel(fields, "Cert C504 Valid From")
	if validFrom.Value != "2023-02-01" {
		t.Errorf("Valid From = %q", validFrom.Value)
	}
	validUntil, _ := fieldByLabel(fields, "Cert C504 Valid Until")
	if validUntil.Value != "2028-01-31" {
		t.Errorf("Valid Until = %q", validUntil.Value)
	}
}

func TestCertFields_NoAlgorithmAvailable(t *testing.T) {
	// Both Certificate and SigAlgOID are absent — should still emit 5 fields,
	// with Algorithm value blank rather than panicking.
	c := &Cert{
		FID:       0xC500,
		Subject:   pkix.Name{CommonName: "Test"},
		Issuer:    pkix.Name{CommonName: "Test-CA"},
		NotBefore: mustParseDate(t, "2024-01-01"),
		NotAfter:  mustParseDate(t, "2025-01-01"),
	}
	fields := certFields(c)
	if len(fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(fields))
	}
	algo, _ := fieldByLabel(fields, "Cert C500 Algorithm")
	if algo.Value != "" {
		t.Errorf("algo value should be empty when no algorithm info, got %q", algo.Value)
	}
}

func TestDiagnosticFields_WithESIGNCerts(t *testing.T) {
	d := &CardData{
		MF: &MFData{ICCSN: "80276000000000000000"},
		ESIGN: &ESIGNData{
			C500: rsaCertFixture(t),
			C504: ecdsaPartialCertFixture(t),
		},
	}
	fields := DiagnosticFields(d)
	// ICCSN + 5 RSA + 5 ECDSA = 11 rows.
	if len(fields) != 11 {
		t.Fatalf("expected 11 fields, got %d", len(fields))
	}
	if _, ok := fieldByLabel(fields, "Cert C500 Subject"); !ok {
		t.Error("missing C500 Subject row")
	}
	if _, ok := fieldByLabel(fields, "Cert C504 Subject"); !ok {
		t.Error("missing C504 Subject row")
	}
}

func TestDiagnosticFields_ESIGNRSAOnly(t *testing.T) {
	d := &CardData{
		MF:    &MFData{ICCSN: "80276000000000000000"},
		ESIGN: &ESIGNData{C500: rsaCertFixture(t)},
	}
	fields := DiagnosticFields(d)
	// ICCSN + 5 RSA rows.
	if len(fields) != 6 {
		t.Fatalf("expected 6 fields, got %d", len(fields))
	}
	if _, ok := fieldByLabel(fields, "Cert C504 Subject"); ok {
		t.Error("unexpected C504 row for RSA-only card")
	}
}

func TestDiagnosticFields_ESIGNECDSAOnly(t *testing.T) {
	d := &CardData{
		MF:    &MFData{ICCSN: "80276000000000000000"},
		ESIGN: &ESIGNData{C504: ecdsaPartialCertFixture(t)},
	}
	fields := DiagnosticFields(d)
	// ICCSN + 5 ECDSA rows.
	if len(fields) != 6 {
		t.Fatalf("expected 6 fields, got %d", len(fields))
	}
	if _, ok := fieldByLabel(fields, "Cert C500 Subject"); ok {
		t.Error("unexpected C500 row for ECDSA-only card")
	}
}

func TestDiagnosticFields_ESIGNEmpty(t *testing.T) {
	// Non-nil ESIGN with both certs nil — no cert rows should appear.
	d := &CardData{
		MF:    &MFData{ICCSN: "80276000000000000000"},
		ESIGN: &ESIGNData{},
	}
	fields := DiagnosticFields(d)
	if len(fields) != 1 {
		t.Fatalf("expected only ICCSN row, got %d fields", len(fields))
	}
	if _, ok := fieldByLabel(fields, "Cert C500 Subject"); ok {
		t.Error("unexpected cert row when ESIGN has no certs")
	}
}

func TestDiagnosticFields_StatusVDWithTimestamp(t *testing.T) {
	d := &CardData{
		MF: &MFData{ICCSN: "80276000000000000000"},
		StatusVD: &StatusVD{
			Timestamp: "2024-06-01T12:00:00",
			StatusHex: "010203",
		},
	}
	fields := DiagnosticFields(d)
	ts, ok := fieldByLabel(fields, "VD letzte Aktualisierung")
	if !ok {
		t.Fatal("missing VD timestamp row")
	}
	if ts.Value != "2024-06-01T12:00:00" {
		t.Errorf("timestamp value = %q", ts.Value)
	}
	if ts.Source != "DF.HCA.EF.StatusVD" {
		t.Errorf("Source = %q", ts.Source)
	}
	raw, ok := fieldByLabel(fields, "VD-Status (raw)")
	if !ok {
		t.Fatal("missing VD-Status (raw) row")
	}
	if raw.Value != "010203" {
		t.Errorf("status hex value = %q", raw.Value)
	}
}

func TestDiagnosticFields_StatusVDUnparsedTimestamp(t *testing.T) {
	// Empty Timestamp → "(unparsed)" placeholder; missing StatusHex → no raw row.
	d := &CardData{
		MF:       &MFData{ICCSN: "80276000000000000000"},
		StatusVD: &StatusVD{},
	}
	fields := DiagnosticFields(d)
	ts, _ := fieldByLabel(fields, "VD letzte Aktualisierung")
	if ts.Value != "(unparsed)" {
		t.Errorf("timestamp value = %q, want (unparsed)", ts.Value)
	}
	if _, ok := fieldByLabel(fields, "VD-Status (raw)"); ok {
		t.Error("status raw row should be suppressed when StatusHex empty")
	}
}

func TestDiagnosticFields_Version2(t *testing.T) {
	d := &CardData{
		MF: &MFData{
			ICCSN: "80276000000000000000",
			Version2: &Version{
				TagC0: "00.00.01",
				TagC1: "00.00.02",
				TagC2: "vendor-string",
				TagC3: "00.00.03",
			},
		},
	}
	fields := DiagnosticFields(d)
	for _, label := range []string{"Version2 / C0", "Version2 / C1", "Version2 / C2", "Version2 / C3"} {
		f, ok := fieldByLabel(fields, label)
		if !ok {
			t.Errorf("missing field %q", label)
			continue
		}
		if f.Source != "MF.EF.Version2" {
			t.Errorf("%s source = %q", label, f.Source)
		}
		if f.Value == "" {
			t.Errorf("%s value should be non-empty", label)
		}
	}
}
