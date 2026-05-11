package egk

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"time"
)

// ESIGNData carries the cardholder X.509 certificates published in
// DF.ESIGN on the eGK. Both certs are publicly readable — no PIN, no C2C.
//
// The eGK G2.x layout puts up to two X.509 certs in DF.ESIGN:
//   - FID C500 — RSA 2048 cert (typically auth / signature)
//   - FID C504 — ECDSA P-256 cert (auth)
//
// Other FIDs in the C500..C50F range (C509/C50A/C50B/C50C) hold CV-certs
// that are PIN-/C2C-protected and not parsed here.
type ESIGNData struct {
	C500 *Cert // RSA 2048 cert (FID C500); nil if missing or unreadable
	C504 *Cert // ECDSA P-256 cert (FID C504); nil if missing or unreadable
}

// Cert is a parsed X.509 certificate plus the raw DER bytes it came from.
// Certificate is non-nil when crypto/x509 could parse the whole cert; when
// the curve is unsupported (brainpoolP256r1 / P384r1, used by gematik) it
// stays nil and the partially-parsed fields below carry the human-relevant
// metadata extracted from the TBSCertificate manually.
type Cert struct {
	FID         uint16
	DER         []byte
	Certificate *x509.Certificate // nil if curve was unsupported by crypto/x509
	Subject     pkix.Name         // always populated when partialParse succeeds
	Issuer      pkix.Name
	NotBefore   time.Time
	NotAfter    time.Time
	SigAlgOID   asn1.ObjectIdentifier // signature algorithm OID from TBS
}

// esignCertFIDs lists the publicly readable X.509 cert FIDs in DF.ESIGN.
// Limited to slots that the live G2.1 probe returned cleanly with READ BINARY
// (C500, C504) — C509..C50C return 6982 and need C2C authentication.
var esignCertFIDs = []uint16{0xC500, 0xC504}

// readESIGN selects DF.ESIGN, reads each known X.509 cert FID, and parses
// the DER bytes. Best-effort: a missing or unreadable cert just yields nil.
// After running, the caller is responsible for re-selecting whichever DF it
// needs next (typically DF.HCA).
func readESIGN(card Card) (*ESIGNData, error) {
	if _, err := selectByAID(card, aidESIGN); err != nil {
		return nil, fmt.Errorf("select DF.ESIGN: %w", err)
	}
	out := &ESIGNData{}
	for _, fid := range esignCertFIDs {
		c, err := readCertEF(card, fid)
		if err != nil {
			if os.Getenv("EGK_TRACE") == "1" {
				fmt.Fprintf(os.Stderr, "[apdu] DF.ESIGN cert %04X: %v\n", fid, err)
			}
			continue
		}
		switch fid {
		case 0xC500:
			out.C500 = c
		case 0xC504:
			out.C504 = c
		}
	}
	return out, nil
}

// readCertEF reads a single X.509-cert EF and parses the DER bytes. It uses
// the DER SEQUENCE length prefix to know exactly how many bytes to fetch
// rather than reading until end-of-file.
func readCertEF(card Card, fid uint16) (*Cert, error) {
	if err := selectEF(card, fid); err != nil {
		return nil, err
	}
	// First read pulls the SEQUENCE header so we know the total length.
	header, err := readBinary(card, 0, 0, 4)
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	total, err := derSequenceTotalLen(header)
	if err != nil {
		return nil, err
	}
	der := make([]byte, 0, total)
	der = append(der, header...)
	for uint32(len(der)) < total {
		remaining := total - uint32(len(der))
		n := uint32(readChunkSize)
		if remaining < n {
			n = remaining
		}
		buf, err := readBinary(card, 0, uint16(len(der)), uint16(n))
		if err != nil {
			return nil, fmt.Errorf("read chunk at %d: %w", len(der), err)
		}
		if len(buf) == 0 {
			break
		}
		der = append(der, buf...)
	}
	c := &Cert{FID: fid, DER: der}
	if cert, err := x509.ParseCertificate(der); err == nil {
		c.Certificate = cert
		c.Subject = cert.Subject
		c.Issuer = cert.Issuer
		c.NotBefore = cert.NotBefore
		c.NotAfter = cert.NotAfter
		return c, nil
	}
	// crypto/x509 rejected the cert (likely brainpool curve). Fall back to a
	// tolerant ASN.1 walk that pulls Subject / Issuer / Validity — none of
	// which need the public key to be parsed.
	if perr := partialParseX509(der, c); perr != nil {
		return nil, fmt.Errorf("parse X.509 (partial): %w", perr)
	}
	return c, nil
}

// tbsView mirrors the prefix of TBSCertificate per RFC 5280 §4.1.2. Mirrors
// crypto/x509's internal tbsCertificate layout so asn1.Unmarshal positions
// each field correctly. SerialNumber is big.Int (not RawValue) because the
// generic RawValue capture eats the next field in some cert encodings.
type tbsView struct {
	Raw                asn1.RawContent
	Version            int `asn1:"optional,explicit,default:0,tag:0"`
	SerialNumber       *big.Int
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Issuer             asn1.RawValue
	Validity           struct {
		NotBefore time.Time
		NotAfter  time.Time
	}
	Subject              asn1.RawValue
	SubjectPublicKeyInfo asn1.RawValue
	// IssuerUniqueId / SubjectUniqueId / Extensions follow but we ignore them.
}

type certView struct {
	Raw                asn1.RawContent
	TBSCertificate     tbsView
	SignatureAlgorithm pkix.AlgorithmIdentifier
	SignatureValue     asn1.BitString
}

func partialParseX509(der []byte, c *Cert) error {
	var cv certView
	if _, err := asn1.Unmarshal(der, &cv); err != nil {
		return err
	}
	// Subject / Issuer are stored as RDNSequence (ASN.1 type); pkix.Name is
	// the parsed-out Go view filled via FillFromRDNSequence.
	var subjRDN, issRDN pkix.RDNSequence
	if _, err := asn1.Unmarshal(cv.TBSCertificate.Subject.FullBytes, &subjRDN); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	c.Subject.FillFromRDNSequence(&subjRDN)
	if _, err := asn1.Unmarshal(cv.TBSCertificate.Issuer.FullBytes, &issRDN); err != nil {
		return fmt.Errorf("issuer: %w", err)
	}
	c.Issuer.FillFromRDNSequence(&issRDN)
	c.NotBefore = cv.TBSCertificate.Validity.NotBefore
	c.NotAfter = cv.TBSCertificate.Validity.NotAfter
	c.SigAlgOID = cv.TBSCertificate.SignatureAlgorithm.Algorithm
	return nil
}

// derSequenceTotalLen returns the full DER-encoded length (header + content)
// for a SEQUENCE introduced by the first few bytes of b. Supports the short
// form and the 0x81 / 0x82 long forms — enough for any cert that fits inside
// a single eGK EF (X.509 certs cap at ~3 KB).
func derSequenceTotalLen(b []byte) (uint32, error) {
	if len(b) < 2 || b[0] != 0x30 {
		return 0, fmt.Errorf("not a DER SEQUENCE: %X", b)
	}
	switch {
	case b[1] < berLenLongMarker:
		return uint32(b[1]) + 2, nil
	case b[1] == berLen1:
		if len(b) < 3 {
			return 0, fmt.Errorf("short DER 0x81 header")
		}
		return uint32(b[2]) + 3, nil
	case b[1] == berLen2:
		if len(b) < 4 {
			return 0, fmt.Errorf("short DER 0x82 header")
		}
		return uint32(binary.BigEndian.Uint16(b[2:4])) + 4, nil
	}
	return 0, fmt.Errorf("unsupported DER length form: %02X", b[1])
}
