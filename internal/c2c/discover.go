package c2c

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/cvcert"
)

// Card is the minimal APDU contract this package needs. It matches
// pkg/reader.Card and pkg/egk.Card structurally — any of those
// satisfies it. We define it locally so c2c doesn't pull in the reader
// package and create an import cycle if reader ever wants to call into c2c.
type Card interface {
	Transmit(apdu []byte) ([]byte, error)
}

// CertSlot identifies one logical location where a CV-cert may live on a card.
// Drivers expose the path to read by combining an AID (the application DF)
// with a FID (the elementary file inside that DF) and optionally a 1-based
// short file identifier (SFI) for SFI-mode reads.
type CertSlot struct {
	Label   string // human-readable hint, e.g. "EF.C.HCI.AUTD_RPS_CVC.E256"
	AID     []byte // application to SELECT before reading; nil = stay at MF
	FID     uint16 // 2-byte EF file identifier
	SFI     byte   // short file identifier (0 = don't use SFI)
	Purpose string // "auth-CVC" | "ca-CVC" | etc. for routing/labelling
}

// KnownSMCBCertSlots lists every FID we know of where an SMC-B is allowed to
// store CV-certs per gemSpec_SMC-B / gemSpec_PKI. Newer card profiles use
// some subset of these; we probe them all and keep the ones that respond.
//
// FIDs sourced from:
//   - gemSpec_SMC-B vN object-system table (EF.C.HCI.AUTD_RPS_CVC.E256 etc.)
//   - gematik test-card service-manuals for the TCOS 2.0 family
//
// CV-certs typically appear under DF.SMA (D27600000144 8000) on production
// SMC-Bs; on TCOS test cards they sometimes live under DF.ESIGN instead.
// We probe both paths.
var KnownSMCBCertSlots = []CertSlot{
	{Label: "EF.C.HCI.AUTD_RPS_CVC.E256 (DF.SMA)", AID: []byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x44, 0x80, 0x00}, FID: 0xC509, Purpose: "auth-CVC"},
	{Label: "EF.C.HCI.AUTR_CVC.E256 (DF.SMA)", AID: []byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x44, 0x80, 0x00}, FID: 0xC50A, Purpose: "auth-CVC"},
	{Label: "EF.C.CA_HCI_OSIG.CS.E256 (DF.SMA)", AID: []byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x44, 0x80, 0x00}, FID: 0xC002, Purpose: "ca-CVC"},
	// DF.ESIGN fallback locations (TCOS test cards)
	{Label: "EF.C.HCI.AUTD_RPS_CVC.E256 (DF.ESIGN)", AID: []byte{0xA0, 0x00, 0x00, 0x01, 0x67, 0x45, 0x53, 0x49, 0x47, 0x4E}, FID: 0xC509, Purpose: "auth-CVC"},
	{Label: "EF.C.HCI.AUTR_CVC.E256 (DF.ESIGN)", AID: []byte{0xA0, 0x00, 0x00, 0x01, 0x67, 0x45, 0x53, 0x49, 0x47, 0x4E}, FID: 0xC50A, Purpose: "auth-CVC"},
}

// KnownEGKCertSlots is the analogous list for the eGK side. Less critical
// for our path (we receive the eGK's CV-cert during MutualAuthenticate;
// we don't need to read it directly), kept for completeness.
var KnownEGKCertSlots = []CertSlot{
	{Label: "EF.C.eGK.AUTD_RPS_CVC.E256 (DF.HCA)", AID: []byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x02}, FID: 0xC500, Purpose: "auth-CVC"},
}

// DiscoveredCert pairs a parsed CV-cert with the slot it was read from.
type DiscoveredCert struct {
	Slot CertSlot
	Cert *cvcert.Cert
	Raw  []byte // raw bytes of the EF body (the 7F21 wrapper or 7F4E body)
}

// ErrCertEFNotFound is returned by discoverOne when the EF doesn't exist on
// the card — distinct from a read error so callers can probe a list of
// candidate slots and treat "not present" as expected.
var ErrCertEFNotFound = errors.New("c2c/discover: cert EF not found")

// DiscoverCVCerts walks the given list of candidate slots, attempting to
// read and parse a CV-cert from each. Returns one DiscoveredCert per slot
// that responded successfully. Slots that return 6A82 / 6985 / similar
// "file not found" / "conditions not satisfied" are silently skipped.
//
// Read order: SELECT MF, SELECT AID (if any), SELECT EF by FID, READ BINARY.
// Read length is bounded by the EF's own DER outer length (CV-cert is a
// 7F21 SEQUENCE with a 1- or 2-byte length).
func DiscoverCVCerts(card Card, slots []CertSlot) ([]DiscoveredCert, []error) {
	var hits []DiscoveredCert
	var errs []error
	seenMF := false
	for _, s := range slots {
		raw, err := readCertEF(card, s, !seenMF)
		seenMF = true
		if errors.Is(err, ErrCertEFNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Label, err))
			continue
		}
		parsed, err := cvcert.Parse(raw)
		if err != nil {
			// Not a CV-cert (or not one we can parse). Could be a sibling
			// X.509 file at the same FID — report it but don't surface as
			// fatal; the caller can decide.
			errs = append(errs, fmt.Errorf("%s: parse: %w", s.Label, err))
			continue
		}
		hits = append(hits, DiscoveredCert{Slot: s, Cert: parsed, Raw: raw})
	}
	return hits, errs
}

// readCertEF performs SELECT MF (if requested) → SELECT AID (if non-nil) →
// SELECT EF → READ BINARY chunked. Returns the raw EF body or
// ErrCertEFNotFound for the typical "not present" SWs.
func readCertEF(card Card, s CertSlot, selectMF bool) ([]byte, error) {
	if selectMF {
		// 00 A4 00 0C 02 3F 00 — SELECT MF, no FCI
		if _, sw, err := transmit(card, []byte{0x00, 0xA4, 0x00, 0x0C, 0x02, 0x3F, 0x00}); err != nil {
			return nil, fmt.Errorf("SELECT MF: %w", err)
		} else if sw != 0x9000 {
			// Some cards don't accept explicit MF select but assume MF
			// after reset; non-fatal.
			_ = sw
		}
	}
	if len(s.AID) > 0 {
		apdu := append([]byte{0x00, 0xA4, 0x04, 0x0C, byte(len(s.AID))}, s.AID...)
		_, sw, err := transmit(card, apdu)
		if err != nil {
			return nil, fmt.Errorf("SELECT AID %X: %w", s.AID, err)
		}
		if sw == 0x6A82 {
			return nil, ErrCertEFNotFound
		}
		if sw != 0x9000 {
			return nil, fmt.Errorf("SELECT AID %X: SW=%04X", s.AID, sw)
		}
	}
	// SELECT EF by FID
	_, sw, err := transmit(card, []byte{0x00, 0xA4, 0x02, 0x0C, 0x02, byte(s.FID >> 8), byte(s.FID & 0xFF)})
	if err != nil {
		return nil, fmt.Errorf("SELECT EF %04X: %w", s.FID, err)
	}
	if sw == 0x6A82 || sw == 0x6A87 {
		return nil, ErrCertEFNotFound
	}
	if sw != 0x9000 {
		return nil, fmt.Errorf("SELECT EF %04X: SW=%04X", s.FID, sw)
	}
	// READ BINARY: first 8 bytes to size the DER outer SEQUENCE, then the
	// rest. The longest CV-cert header is `7F 21 82 LL LL` = 5 bytes; 8
	// is a safe round number that also covers any high-tag-number form.
	head, sw, err := readBinary(card, 0, 8)
	if err != nil {
		return nil, fmt.Errorf("READ BINARY head: %w", err)
	}
	if sw != 0x9000 || len(head) < 2 {
		return nil, fmt.Errorf("READ BINARY head: SW=%04X data=%X", sw, head)
	}
	total, hdrLen, err := derTotalLen(head)
	if err != nil {
		return nil, fmt.Errorf("DER length parse: %w (head=%X)", err, head)
	}
	_ = hdrLen
	out := make([]byte, 0, total)
	// Keep every byte we already pulled in the head read — the loop below
	// continues from len(out), so dropping any would cause the next chunk
	// to overlap or skip body bytes.
	keep := len(head)
	if uint16(keep) > total {
		keep = int(total)
	}
	out = append(out, head[:keep]...)
	for uint16(len(out)) < total {
		remaining := total - uint16(len(out))
		n := uint16(0xFA)
		if remaining < n {
			n = remaining
		}
		chunk, csw, err := readBinary(card, uint16(len(out)), byte(n))
		if err != nil {
			return nil, fmt.Errorf("READ BINARY off=%d: %w", len(out), err)
		}
		if csw != 0x9000 && csw != 0x6282 {
			return nil, fmt.Errorf("READ BINARY off=%d: SW=%04X", len(out), csw)
		}
		out = append(out, chunk...)
		if len(chunk) == 0 {
			break
		}
	}
	return out, nil
}

// derTotalLen inspects up to 4 bytes of a DER-encoded TLV and returns the
// total bytes the value occupies on the card (tag + length-of-length +
// content). The CV-cert wrapper is `7F21 [LL[LL]] body`, so we parse one
// multi-byte tag then the length.
func derTotalLen(head []byte) (total uint16, hdrConsumed int, err error) {
	if len(head) < 2 {
		return 0, 0, errors.New("short")
	}
	// Multi-byte tag (high-tag-number form) — first byte's low 5 bits = 0x1F.
	i := 0
	if head[i]&0x1F == 0x1F {
		i++
		for i < len(head) {
			more := head[i]&0x80 != 0
			i++
			if !more {
				break
			}
		}
	} else {
		i++
	}
	if i >= len(head) {
		return 0, 0, errors.New("tag overflow")
	}
	lenByte := head[i]
	i++
	var length uint16
	switch {
	case lenByte < 0x80:
		length = uint16(lenByte)
	case lenByte == 0x81:
		if i >= len(head) {
			return 0, 0, errors.New("len81 overflow")
		}
		length = uint16(head[i])
		i++
	case lenByte == 0x82:
		if i+1 >= len(head) {
			return 0, 0, errors.New("len82 overflow")
		}
		length = binary.BigEndian.Uint16(head[i : i+2])
		i += 2
	default:
		return 0, 0, fmt.Errorf("unsupported length form %02X", lenByte)
	}
	return uint16(i) + length, i, nil
}

// transmit is the local APDU helper: returns (data, SW, err) where data
// excludes the trailing SW1SW2.
func transmit(card Card, apdu []byte) ([]byte, uint16, error) {
	resp, err := card.Transmit(apdu)
	if err != nil {
		return nil, 0, err
	}
	if len(resp) < 2 {
		return nil, 0, fmt.Errorf("short response: %X", resp)
	}
	sw := binary.BigEndian.Uint16(resp[len(resp)-2:])
	return resp[:len(resp)-2], sw, nil
}

// readBinary sends `00 B0 P1 P2 Le` for the currently selected EF where
// P1P2 is the offset and Le is the requested length. Returns the data
// payload, SW, and any I/O error.
func readBinary(card Card, offset uint16, le byte) ([]byte, uint16, error) {
	return transmit(card, []byte{0x00, 0xB0, byte(offset >> 8), byte(offset & 0xFF), le})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
