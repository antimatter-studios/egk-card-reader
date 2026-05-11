package c2c

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// fakeCard plays back a scripted APDU sequence. APDUs are matched by prefix
// (request CLA INS P1 P2 plus optional Lc/data) so callers can register
// generic SELECT/READ handlers. The first matching script wins.
type fakeCard struct {
	scripts []scriptEntry
	calls   [][]byte
}

type scriptEntry struct {
	match []byte
	resp  []byte
}

func (f *fakeCard) Transmit(apdu []byte) ([]byte, error) {
	f.calls = append(f.calls, append([]byte(nil), apdu...))
	for _, s := range f.scripts {
		if len(apdu) >= len(s.match) && bytes.Equal(apdu[:len(s.match)], s.match) {
			return append([]byte(nil), s.resp...), nil
		}
	}
	return nil, errors.New("fakeCard: no scripted response for " + hex.EncodeToString(apdu))
}

func (f *fakeCard) reset() { f.calls = nil }

func mustHex(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

// minimalCV builds a tiny but parseable RSA-2048 CV-cert wrapper (7F21
// outer). Enough body to make cvcert.Parse return a non-nil Cert.
func minimalCV(car, chr string) []byte {
	cpi := mustHex("5F2901 70")
	carTLV := append([]byte{0x42, byte(len(car))}, []byte(car)...)
	chrTLV := append([]byte{0x5F, 0x20, byte(len(chr))}, []byte(chr)...)
	// BSI TR-03110 "packed BCD": one decimal digit per byte (low nibble),
	// 6 bytes = YYMMDD. 2025-01-01 → 02 05 00 01 00 01.
	notBefore := mustHex("5F2506 02 05 00 01 00 01")
	notAfter := mustHex("5F2406 03 00 01 02 03 01") // 2030-12-31

	pkBody := mustHex("06 0A 04 00 7F 00 07 02 02 02 01 02" + // OID RSA2048
		"81 81 01 99" + // tag 81 length form 81, length 1 — modulus (stubbed 1 byte)
		"82 03 01 00 01") // tag 82 — exponent 65537
	pkTLV := append([]byte{0x7F, 0x49, 0x81, byte(len(pkBody))}, pkBody...)
	body := bytes.Join([][]byte{cpi, carTLV, pkTLV, chrTLV, notBefore, notAfter}, nil)
	bodyTLV := append([]byte{0x7F, 0x4E, 0x81, byte(len(body))}, body...)
	sig := []byte{0x5F, 0x37, 0x10}
	sig = append(sig, bytes.Repeat([]byte{0xAA}, 16)...)
	cert := append(bodyTLV, sig...)
	wrapper := append([]byte{0x7F, 0x21, 0x82, byte(len(cert) >> 8), byte(len(cert) & 0xFF)}, cert...)
	return wrapper
}

func TestDiscoverCVCerts_FindsCertInScriptedSlot(t *testing.T) {
	cv := minimalCV("MY-CA", "MY-LEAF")

	// Build the head we'll return when the test reads the first 8 bytes:
	// our wrapper starts 7F 21 82 LL LL — the first 5 are the header.
	// Copy so the later `append(head, sw...)` can't mutate cv's backing
	// array via the implicit shared capacity.
	head := append([]byte(nil), cv[:8]...)
	// Total bytes claimed by DER: 7F 21 = tag (2 bytes), 82 LL LL = 3-byte length, then content.
	// derTotalLen returns total = hdrConsumed + length where length is in 7F 21 wrapper.
	// We just need the chunk reads to deliver the rest correctly.

	card := &fakeCard{
		scripts: []scriptEntry{
			// SELECT MF
			{match: mustHex("00 A4 00 0C 02 3F 00"), resp: mustHex("9000")},
			// SELECT DF.SMA AID
			{match: mustHex("00 A4 04 0C 08 D2 76 00 00 01 44 80 00"), resp: mustHex("9000")},
			// SELECT EF C509 (auth CVC)
			{match: mustHex("00 A4 02 0C 02 C5 09"), resp: mustHex("9000")},
			// READ BINARY at offset 0, Le=08 — return our 8-byte head + 9000
			{match: mustHex("00 B0 00 00 08"), resp: append(head, 0x90, 0x00)},
			// All other SELECT EF candidates: not found
			{match: mustHex("00 A4 02 0C 02"), resp: mustHex("6A82")},
		},
	}

	// Add a catch-all READ BINARY that returns chunks of the CV-cert body.
	// We'll wire a custom Transmit by inheritance? Simpler: append explicit
	// chunk responses after offset 0.
	// Compute remaining bytes after head[:hdrConsumed (=5)] and stream them.
	totalLen, hdrConsumed, err := derTotalLen(head)
	if err != nil {
		t.Fatalf("derTotalLen on head %X: %v", head, err)
	}
	_ = hdrConsumed
	if int(totalLen) != len(cv) {
		t.Logf("computed totalLen=%d, len(cv)=%d; rolling with computed", totalLen, len(cv))
	}

	// READ BINARY at offset 8, Le=FA (max 250) — return rest of cv + 9000.
	off := 8
	for off < len(cv) {
		n := len(cv) - off
		if n > 0xFA {
			n = 0xFA
		}
		chunkAPDU := mustHex("00 B0")
		chunkAPDU = append(chunkAPDU, byte(off>>8), byte(off&0xFF), byte(n))
		card.scripts = append(card.scripts, scriptEntry{
			match: chunkAPDU,
			resp:  append(append([]byte{}, cv[off:off+n]...), 0x90, 0x00),
		})
		off += n
	}

	hits, errs := DiscoverCVCerts(card, KnownSMCBCertSlots[:1]) // just the first slot
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1; errs=%v", len(hits), errs)
	}
	if hits[0].Cert.CAR != "MY-CA" {
		t.Errorf("CAR = %q, want MY-CA", hits[0].Cert.CAR)
	}
	if hits[0].Cert.CHR != "MY-LEAF" {
		t.Errorf("CHR = %q, want MY-LEAF", hits[0].Cert.CHR)
	}
	if !bytes.Equal(hits[0].Raw, cv) {
		t.Errorf("Raw mismatch:\ngot:  %X\nwant: %X", hits[0].Raw, cv)
	}
}

func TestDiscoverCVCerts_NoSlotsResponding(t *testing.T) {
	// All SELECT EF return 6A82 (file not found).
	card := &fakeCard{
		scripts: []scriptEntry{
			{match: mustHex("00 A4 00 0C 02 3F 00"), resp: mustHex("9000")},
			{match: mustHex("00 A4 04 0C"), resp: mustHex("9000")},   // any AID
			{match: mustHex("00 A4 02 0C"), resp: mustHex("6A82")},   // any EF select
		},
	}
	hits, _ := DiscoverCVCerts(card, KnownSMCBCertSlots)
	if len(hits) != 0 {
		t.Errorf("expected no hits, got %d", len(hits))
	}
}

func TestDerTotalLen(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          string
		wantTotal   uint16
		wantHdr     int
		wantErr     bool
	}{
		{"short form", "30 05 00 00 00 00 00", 7, 2, false},
		{"long form 81", "30 81 0A 00", 13, 3, false},
		{"long form 82 multi-byte tag", "7F 21 82 01 99", 414, 5, false},
		{"truncated", "30", 0, 0, true},
		{"bad length form (>=84)", "30 84 00 00 00 00", 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := mustHex(tc.in)
			got, hdr, err := derTotalLen(b)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got != tc.wantTotal {
				t.Errorf("total = %d, want %d", got, tc.wantTotal)
			}
			if hdr != tc.wantHdr {
				t.Errorf("hdrConsumed = %d, want %d", hdr, tc.wantHdr)
			}
		})
	}
}
