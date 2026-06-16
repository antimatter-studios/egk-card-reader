package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/antimatter-studios/egk-card-reader/pkg/reader/orga"
)

// runReadCert: SELECT DF.ESIGN (or any AID), SELECT EF by FID, READ BINARY
// chunked until SW != 9000, parse as X.509 if asked, save to file optionally.
// The card is responsible for advertising its file length via the DER outer
// SEQUENCE — we trust `30 82 LL LL` to size the read.
func runReadCert(t *orga.Terminal, slot int, aidHex, fidHex string, outPath string, parse bool) error {
	s := t.Slot(slot)

	// Navigate: MF, AID (if given), EF FID
	if _, err := s.Transmit([]byte{0x00, 0xA4, 0x00, 0x0C, 0x02, 0x3F, 0x00}); err != nil {
		return fmt.Errorf("SELECT MF: %w", err)
	}
	if aidHex != "" {
		aid, err := decodeHex(aidHex)
		if err != nil {
			return fmt.Errorf("aid: %w", err)
		}
		apdu := append([]byte{0x00, 0xA4, 0x04, 0x0C, byte(len(aid))}, aid...)
		resp, err := s.Transmit(apdu)
		if err != nil || lastSW(resp) != 0x9000 {
			return fmt.Errorf("SELECT AID %X: SW=%04X err=%v", aid, lastSW(resp), err)
		}
	}
	if fidHex == "" {
		return fmt.Errorf("-fid required")
	}
	fid, err := decodeHex(fidHex)
	if err != nil || len(fid) != 2 {
		return fmt.Errorf("fid must be 2 hex bytes")
	}
	resp, err := s.Transmit([]byte{0x00, 0xA4, 0x02, 0x0C, 0x02, fid[0], fid[1]})
	if err != nil || lastSW(resp) != 0x9000 {
		return fmt.Errorf("SELECT EF %X: SW=%04X err=%v", fid, lastSW(resp), err)
	}

	// Peek 4 bytes to get DER length.
	head, sw, err := readChunk(s, 0, 4)
	if err != nil || sw != 0x9000 {
		return fmt.Errorf("read header: SW=%04X err=%v", sw, err)
	}
	if len(head) < 4 || head[0] != 0x30 {
		return fmt.Errorf("not a DER SEQUENCE (got %X)", head)
	}
	var total uint16
	switch head[1] {
	case 0x81:
		total = uint16(head[2]) + 3
		head = head[:3]
	case 0x82:
		total = uint16(head[2])<<8 | uint16(head[3]) + 4
	default:
		if head[1] < 0x80 {
			total = uint16(head[1]) + 2
			head = head[:2]
		} else {
			return fmt.Errorf("unsupported DER length form %02X", head[1])
		}
	}
	fmt.Fprintf(os.Stderr, "EF length: %d bytes\n", total)

	der := make([]byte, 0, total)
	der = append(der, head...)
	for uint16(len(der)) < total {
		off := uint16(len(der))
		remaining := total - off
		n := uint16(0xFA)
		if remaining < n {
			n = remaining
		}
		chunk, csw, err := readChunk(s, off, byte(n))
		if err != nil {
			return fmt.Errorf("read off=%d: %w", off, err)
		}
		if csw != 0x9000 && csw != 0x6282 {
			return fmt.Errorf("read off=%d SW=%04X", off, csw)
		}
		der = append(der, chunk...)
		if len(chunk) == 0 {
			break
		}
	}

	fmt.Fprintf(os.Stderr, "read %d bytes\n", len(der))

	if outPath != "" {
		if err := os.WriteFile(outPath, der, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", outPath)
		// Also write PEM next to it for convenience.
		pemPath := strings.TrimSuffix(outPath, ".der") + ".pem"
		if pemPath != outPath {
			pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
			_ = os.WriteFile(pemPath, pemBytes, 0o644)
			fmt.Fprintf(os.Stderr, "wrote %s\n", pemPath)
		}
	}

	if parse {
		return parseAndPrintCert(os.Stdout, der)
	}
	return nil
}

func readChunk(s *orga.Slot, off uint16, le byte) ([]byte, uint16, error) {
	apdu := []byte{0x00, 0xB0, byte(off >> 8), byte(off & 0xFF), le}
	resp, err := s.Transmit(apdu)
	if err != nil {
		return nil, 0, err
	}
	if len(resp) < 2 {
		return nil, 0, fmt.Errorf("short response %X", resp)
	}
	return resp[:len(resp)-2], lastSW(resp), nil
}

func lastSW(resp []byte) uint16 {
	if len(resp) < 2 {
		return 0
	}
	return uint16(resp[len(resp)-2])<<8 | uint16(resp[len(resp)-1])
}

func parseAndPrintCert(w io.Writer, der []byte) error {
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return fmt.Errorf("x509 parse: %w", err)
	}
	fmt.Fprintf(w, "## X.509 certificate\n\n")
	fmt.Fprintf(w, "- Serial: `%s`\n", c.SerialNumber.String())
	fmt.Fprintf(w, "- Subject: %s\n", c.Subject.String())
	fmt.Fprintf(w, "- Issuer:  %s\n", c.Issuer.String())
	fmt.Fprintf(w, "- Valid:   %s → %s\n", c.NotBefore.Format("2006-01-02"), c.NotAfter.Format("2006-01-02"))
	fmt.Fprintf(w, "- Key alg: %s\n", c.PublicKeyAlgorithm)
	fmt.Fprintf(w, "- Sig alg: %s\n", c.SignatureAlgorithm)
	if len(c.SubjectKeyId) > 0 {
		fmt.Fprintf(w, "- SubjectKeyId: `%X`\n", c.SubjectKeyId)
	}
	if len(c.AuthorityKeyId) > 0 {
		fmt.Fprintf(w, "- AuthorityKeyId: `%X`\n", c.AuthorityKeyId)
	}
	if len(c.CRLDistributionPoints) > 0 {
		fmt.Fprintf(w, "- CRL DPs:\n")
		for _, u := range c.CRLDistributionPoints {
			fmt.Fprintf(w, "  - %s\n", u)
		}
	}
	if len(c.IssuingCertificateURL) > 0 {
		fmt.Fprintf(w, "- AIA cert URLs:\n")
		for _, u := range c.IssuingCertificateURL {
			fmt.Fprintf(w, "  - %s\n", u)
		}
	}
	if len(c.OCSPServer) > 0 {
		fmt.Fprintf(w, "- OCSP:\n")
		for _, u := range c.OCSPServer {
			fmt.Fprintf(w, "  - %s\n", u)
		}
	}
	return nil
}
