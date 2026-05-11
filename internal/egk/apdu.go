package egk

import (
	"encoding/binary"
	"fmt"
	"os"
)

// AID for the Health Card Application (DF.HCA) on the German eGK.
var aidHCA = []byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x02}

// MF (Master File) FID, useful when we need to reset position to root.
const fidMF = 0x3F00

// Short File Identifiers within DF.HCA (gemSpec_eGK_ObjSys).
// SFI access lets us READ BINARY without a separate SELECT EF.
const (
	sfiPD = 0x01 // EF.PD
	sfiVD = 0x02 // EF.VD
)

// File identifiers within DF.HCA.
const (
	efPD = 0x2F01 // Personal Data (Persönliche Versichertendaten)
	efVD = 0x2F02 // Insurance Data (Allgemeine + Geschützte VD)
)

// trace prints APDU exchanges when EGK_TRACE=1.
func trace(format string, args ...any) {
	if os.Getenv("EGK_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[apdu] "+format+"\n", args...)
	}
}

// transmit sends an APDU and returns the response data plus SW1SW2.
func transmit(card Card, apdu []byte) ([]byte, uint16, error) {
	trace(">> %X", apdu)
	rsp, err := card.Transmit(apdu)
	if err != nil {
		return nil, 0, err
	}
	if len(rsp) < 2 {
		return nil, 0, fmt.Errorf("short response: %x", rsp)
	}
	sw := uint16(rsp[len(rsp)-2])<<8 | uint16(rsp[len(rsp)-1])
	data := rsp[:len(rsp)-2]
	trace("<< SW=%04X data=%X", sw, data)
	return data, sw, nil
}

// selectMF selects the Master File (root). Useful to reset before re-selecting.
func selectMF(card Card) error {
	// 00 A4 00 0C 02 3F 00 — select by FID, no FCI
	apdu := []byte{0x00, 0xA4, 0x00, 0x0C, 0x02, 0x3F, 0x00}
	_, sw, err := transmit(card, apdu)
	if err != nil {
		return err
	}
	// Some cards return MF only via empty SELECT — try that fallback.
	if sw != 0x9000 {
		_, sw, err = transmit(card, []byte{0x00, 0xA4, 0x00, 0x0C})
		if err != nil {
			return err
		}
		if sw != 0x9000 {
			return fmt.Errorf("SELECT MF failed: SW=%04X", sw)
		}
	}
	return nil
}

// selectByAID selects an application by its AID. Uses P2=0x04 (return FCP) on
// retry to surface FCI data so callers can verify which DF was selected.
func selectByAID(card Card, aid []byte) ([]byte, error) {
	// Try with FCP first — gives us back a TLV with the selected DF's metadata.
	apdu := append([]byte{0x00, 0xA4, 0x04, 0x04, byte(len(aid))}, aid...)
	apdu = append(apdu, 0x00) // Le=0 → return up to 256 bytes of FCP
	data, sw, err := transmit(card, apdu)
	if err != nil {
		return nil, err
	}
	if sw>>8 == 0x61 {
		// Need GET RESPONSE — older cards
		more, _, gerr := transmit(card, []byte{0x00, 0xC0, 0x00, 0x00, byte(sw & 0xFF)})
		if gerr == nil {
			data = append(data, more...)
		}
		sw = 0x9000
	}
	if sw != 0x9000 {
		// Fallback: P2=0x0C (no response data) for cards that reject FCP request.
		apdu = append([]byte{0x00, 0xA4, 0x04, 0x0C, byte(len(aid))}, aid...)
		_, sw, err = transmit(card, apdu)
		if err != nil {
			return nil, err
		}
		if sw != 0x9000 {
			return nil, fmt.Errorf("SELECT AID failed: SW=%04X", sw)
		}
		return nil, nil
	}
	return data, nil
}

// selectEF selects an Elementary File by its 2-byte file ID.
func selectEF(card Card, fid uint16) error {
	apdu := []byte{0x00, 0xA4, 0x02, 0x0C, 0x02, byte(fid >> 8), byte(fid & 0xFF)}
	_, sw, err := transmit(card, apdu)
	if err != nil {
		return err
	}
	if sw != 0x9000 {
		return fmt.Errorf("SELECT EF %04X failed: SW=%04X", fid, sw)
	}
	return nil
}

// readBinary reads `length` bytes starting at offset within the currently
// selected EF (sfi=0), or — when sfi != 0 — within the EF designated by SFI.
// Per ISO 7816-4, READ BINARY with P1.b8=1 uses P1.b5..b1 as SFI and P2 as a
// 1-byte offset (0..255), so for SFI mode the offset is limited to a byte.
func readBinary(card Card, sfi byte, offset, length uint16) ([]byte, error) {
	var p1, p2 byte
	if sfi != 0 {
		if offset > 0xFF {
			return nil, fmt.Errorf("SFI offset %d > 255", offset)
		}
		p1 = 0x80 | (sfi & 0x1F)
		p2 = byte(offset)
	} else {
		p1 = byte(offset >> 8)
		p2 = byte(offset)
	}
	apdu := []byte{0x00, 0xB0, p1, p2, byte(length & 0xFF)}
	data, sw, err := transmit(card, apdu)
	if err != nil {
		return nil, err
	}
	if sw>>8 == 0x6C {
		apdu[4] = byte(sw & 0xFF)
		data, sw, err = transmit(card, apdu)
		if err != nil {
			return nil, err
		}
	}
	if sw != 0x9000 && sw != 0x6282 /* end of file */ {
		return nil, fmt.Errorf("READ BINARY (sfi=%02X off=%04X) failed: SW=%04X", sfi, offset, sw)
	}
	return data, nil
}

// readEFBySFI reads a full EF using SFI-based access. The first 2 bytes of the
// EF (PD layout) or the four 2-byte offset pointers (VD layout) tell us how
// much to read; everything else is just chunked READ BINARY.
//
// SFI access has a 1-byte (255) offset cap, so once the read position passes
// 255 we have to switch to FID-based addressing — fall back to selectEF then
// keep reading.
func readEFBySFI(card Card, sfi byte, fid uint16) ([]byte, error) {
	header, err := readBinary(card, sfi, 0, 8)
	if err != nil {
		return nil, err
	}
	if len(header) < 2 {
		return nil, fmt.Errorf("EF SFI=%02X header too short", sfi)
	}

	var total uint16
	if sfi == sfiPD {
		total = binary.BigEndian.Uint16(header[:2]) + 2
	} else {
		end1 := binary.BigEndian.Uint16(header[2:4])
		end2 := binary.BigEndian.Uint16(header[6:8])
		total = end1
		if end2 > total {
			total = end2
		}
		total++
	}

	out := make([]byte, 0, total)
	out = append(out, header...)
	const chunk = 0xFC
	for uint16(len(out)) < total {
		remaining := total - uint16(len(out))
		n := uint16(chunk)
		if remaining < n {
			n = remaining
		}
		off := uint16(len(out))

		// SFI-mode READ BINARY caps the offset at 255 (P2 is one byte). Once
		// past 255 we drop the SFI bit — but the EF is already implicitly the
		// current EF from the first SFI read (ISO 7816-4 §7.2.3), so plain
		// READ BINARY with a 15-bit offset continues to work.
		var buf []byte
		if off <= 0xFF {
			buf, err = readBinary(card, sfi, off, n)
		} else {
			buf, err = readBinary(card, 0, off, n)
		}
		if err != nil {
			return nil, err
		}
		if len(buf) == 0 {
			break
		}
		out = append(out, buf...)
	}
	return out, nil
}
