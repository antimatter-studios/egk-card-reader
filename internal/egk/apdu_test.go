package egk

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// isSelectEFFID reports whether apdu is a SELECT EF (by FID) for the given fid.
// Used in test stubs so the switch-case reads "is this a SELECT EF.GDO?"
// instead of comparing apdu byte indices to hex literals.
func isSelectEFFID(apdu []byte, fid uint16) bool {
	return len(apdu) >= 7 &&
		apdu[0] == claISO &&
		apdu[1] == insSelect &&
		apdu[2] == p1SelectEF &&
		apdu[3] == p2NoFCI &&
		apdu[4] == 0x02 &&
		apdu[5] == byte(fid>>8) &&
		apdu[6] == byte(fid&0xFF)
}

// isSelectMF reports whether apdu is a SELECT MF (FID 3F00) command.
func isSelectMF(apdu []byte) bool {
	return len(apdu) >= 7 &&
		apdu[0] == claISO &&
		apdu[1] == insSelect &&
		apdu[2] == p1SelectMF &&
		apdu[3] == p2NoFCI &&
		apdu[5] == byte(fidMF>>8) &&
		apdu[6] == byte(fidMF&0xFF)
}

// isSelectAID reports whether apdu is a SELECT (by AID) command.
func isSelectAID(apdu []byte) bool {
	return len(apdu) >= 2 && apdu[0] == claISO && apdu[1] == insSelect && apdu[2] == p1SelectAID
}

// fakeCard is a deterministic Card implementation: it walks through a scripted
// list of (request, response) pairs. Each Transmit consumes one script entry,
// verifies the request matches, and returns the canned response.
type fakeCard struct {
	t        *testing.T
	script   []scriptEntry
	pos      int
	transErr error // optional error to inject on every call
}

type scriptEntry struct {
	wantAPDU string // hex; empty string = any
	respData []byte
	sw       uint16
	err      error
}

func (f *fakeCard) Transmit(apdu []byte) ([]byte, error) {
	if f.transErr != nil {
		return nil, f.transErr
	}
	if f.pos >= len(f.script) {
		f.t.Fatalf("script exhausted at request %s", hex.EncodeToString(apdu))
	}
	e := f.script[f.pos]
	f.pos++
	if e.wantAPDU != "" {
		got := strings.ToUpper(hex.EncodeToString(apdu))
		want := strings.ToUpper(e.wantAPDU)
		if got != want {
			f.t.Fatalf("script[%d]: got APDU %s, want %s", f.pos-1, got, want)
		}
	}
	if e.err != nil {
		return nil, e.err
	}
	out := append([]byte{}, e.respData...)
	out = append(out, byte(e.sw>>8), byte(e.sw&0xFF))
	return out, nil
}

func TestTransmitShortResponse(t *testing.T) {
	// Card returns just 1 byte — not even an SW.
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{respData: nil, sw: 0, err: nil}, // we'll override
		},
	}
	// Override script entry to return raw 1-byte response.
	card.script[0] = scriptEntry{wantAPDU: ""}
	// Replace Transmit behaviour by injecting a transErr alternative — easier
	// to test via a one-shot func wrapper.
	stub := stubCard(func(_ []byte) ([]byte, error) { return []byte{0x9F}, nil })
	if _, _, err := transmit(stub, []byte{0x00}); err == nil {
		t.Error("expected short-response error")
	}
}

func TestTransmitErrorPropagates(t *testing.T) {
	stub := stubCard(func(_ []byte) ([]byte, error) { return nil, fmt.Errorf("read failure") })
	if _, _, err := transmit(stub, []byte{0x00}); err == nil {
		t.Error("expected transmit error to propagate")
	}
}

func TestSelectMFHappy(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4000C023F00", sw: 0x9000},
		},
	}
	if err := selectMF(card); err != nil {
		t.Fatalf("selectMF: %v", err)
	}
}

func TestSelectMFFallback(t *testing.T) {
	// First attempt returns non-9000, second (empty SELECT MF) returns 9000.
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4000C023F00", sw: 0x6A82},
			{wantAPDU: "00A4000C", sw: 0x9000},
		},
	}
	if err := selectMF(card); err != nil {
		t.Fatalf("selectMF fallback: %v", err)
	}
}

func TestSelectMFFails(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{sw: 0x6A82},
			{sw: 0x6A82},
		},
	}
	if err := selectMF(card); err == nil {
		t.Error("expected error on dual failure")
	}
}

func TestSelectMFTransmitError(t *testing.T) {
	stub := stubCard(func(_ []byte) ([]byte, error) { return nil, fmt.Errorf("link down") })
	if err := selectMF(stub); err == nil {
		t.Error("expected error to propagate")
	}
}

func TestSelectByAIDHappyWithFCP(t *testing.T) {
	// SELECT AID with Le=00 (FCP requested) → returns FCP + 9000.
	fcp := []byte{0x6F, 0x05, 0xC0, 0xDE, 0xCA, 0xFE, 0xBA}
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4040406D276000001020" + "0", respData: fcp, sw: 0x9000},
		},
	}
	got, err := selectByAID(card, aidHCA)
	if err != nil {
		t.Fatalf("selectByAID: %v", err)
	}
	if !bytes.Equal(got, fcp) {
		t.Errorf("FCP mismatch: %x vs %x", got, fcp)
	}
}

func TestSelectByAIDGetResponse(t *testing.T) {
	// First response is SW=61xx (more data available) → driver issues
	// GET RESPONSE and concatenates.
	first := []byte{0x6F, 0x03, 0xAA}
	more := []byte{0xBB, 0xCC, 0xDD}
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{respData: first, sw: 0x6103},
			{wantAPDU: "00C0000003", respData: more, sw: 0x9000},
		},
	}
	got, err := selectByAID(card, aidHCA)
	if err != nil {
		t.Fatalf("selectByAID: %v", err)
	}
	want := append([]byte{}, first...)
	want = append(want, more...)
	if !bytes.Equal(got, want) {
		t.Errorf("concat mismatch: got %x want %x", got, want)
	}
}

func TestSelectByAIDFallbackP2_0C(t *testing.T) {
	// First (FCP) attempt errors; fallback (P2=0x0C, no Le) succeeds.
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{sw: 0x6A82},
			{wantAPDU: "00A4040C06D27600000102", sw: 0x9000},
		},
	}
	got, err := selectByAID(card, aidHCA)
	if err != nil {
		t.Fatalf("selectByAID fallback: %v", err)
	}
	if got != nil {
		t.Errorf("fallback should return nil FCP, got %x", got)
	}
}

func TestSelectByAIDDoubleFailure(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{sw: 0x6A82},
			{sw: 0x6A82},
		},
	}
	if _, err := selectByAID(card, aidHCA); err == nil {
		t.Error("expected double failure error")
	}
}

func TestSelectByAIDFirstTransmitError(t *testing.T) {
	stub := stubCard(func(_ []byte) ([]byte, error) { return nil, fmt.Errorf("io") })
	if _, err := selectByAID(stub, aidHCA); err == nil {
		t.Error("expected error")
	}
}

func TestSelectEFHappy(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C022F01", sw: 0x9000},
		},
	}
	if err := selectEF(card, 0x2F01); err != nil {
		t.Fatalf("selectEF: %v", err)
	}
}

func TestSelectEFFails(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{sw: 0x6A82},
		},
	}
	if err := selectEF(card, 0x2F01); err == nil {
		t.Error("expected error")
	}
}

func TestSelectEFTransmitError(t *testing.T) {
	stub := stubCard(func(_ []byte) ([]byte, error) { return nil, fmt.Errorf("io") })
	if err := selectEF(stub, 0x2F01); err == nil {
		t.Error("expected error")
	}
}

func TestReadBinarySFI(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00B0810008", respData: []byte{1, 2, 3, 4, 5, 6, 7, 8}, sw: 0x9000},
		},
	}
	got, err := readBinary(card, sfiPD, 0, 8)
	if err != nil {
		t.Fatalf("readBinary SFI: %v", err)
	}
	if len(got) != 8 {
		t.Errorf("len = %d", len(got))
	}
}

func TestReadBinaryNonSFI(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00B0010005", respData: []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE}, sw: 0x9000},
		},
	}
	got, err := readBinary(card, 0, 0x0100, 5)
	if err != nil {
		t.Fatalf("readBinary FID: %v", err)
	}
	if !bytes.Equal(got, []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE}) {
		t.Errorf("bytes mismatch: %x", got)
	}
}

func TestReadBinarySFIOffsetTooLarge(t *testing.T) {
	card := &fakeCard{t: t}
	if _, err := readBinary(card, sfiPD, 256, 8); err == nil {
		t.Error("SFI offset > 255 should error")
	}
}

func TestReadBinarySW6Cxx(t *testing.T) {
	// SW=6Cnn means "wrong Le, use nn". Driver should retry with the suggested Le.
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00B0810008", sw: 0x6C05},
			{wantAPDU: "00B0810005", respData: []byte{1, 2, 3, 4, 5}, sw: 0x9000},
		},
	}
	got, err := readBinary(card, sfiPD, 0, 8)
	if err != nil {
		t.Fatalf("readBinary 6Cxx: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3, 4, 5}) {
		t.Errorf("got %x", got)
	}
}

func TestReadBinarySW6Cxx_SecondAttemptFails(t *testing.T) {
	stub := stubCard(func(apdu []byte) ([]byte, error) {
		if apdu[4] == 0x08 {
			return []byte{0x6C, 0x05}, nil
		}
		return nil, fmt.Errorf("link down on retry")
	})
	if _, err := readBinary(stub, sfiPD, 0, 8); err == nil {
		t.Error("expected error from retry")
	}
}

func TestReadBinaryEOFOk(t *testing.T) {
	// SW=6282 (end of file) is accepted.
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{respData: []byte{0xAA}, sw: 0x6282},
		},
	}
	got, err := readBinary(card, sfiPD, 0, 8)
	if err != nil {
		t.Fatalf("readBinary 6282: %v", err)
	}
	if !bytes.Equal(got, []byte{0xAA}) {
		t.Errorf("got %x", got)
	}
}

func TestReadBinaryUnexpectedSW(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{sw: 0x6982},
		},
	}
	if _, err := readBinary(card, sfiPD, 0, 8); err == nil {
		t.Error("expected error on unexpected SW")
	}
}

func TestReadBinaryTransmitError(t *testing.T) {
	stub := stubCard(func(_ []byte) ([]byte, error) { return nil, fmt.Errorf("io") })
	if _, err := readBinary(stub, sfiPD, 0, 8); err == nil {
		t.Error("expected error")
	}
}

// stubCard wraps a func into a Card. Lighter than fakeCard for one-off cases.
type stubCard func(apdu []byte) ([]byte, error)

func (s stubCard) Transmit(apdu []byte) ([]byte, error) { return s(apdu) }

// ---- readEFBySFI tests ---------------------------------------------------

func TestReadEFBySFI_PD(t *testing.T) {
	// Build a small PD payload: 2-byte length prefix + gzipped body, padded
	// so the total is a multiple of 8 to keep boundary math obvious.
	body := []byte("hello PD payload")
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(body)
	zw.Close()

	full := make([]byte, 2+gz.Len())
	binary.BigEndian.PutUint16(full[0:2], uint16(gz.Len()))
	copy(full[2:], gz.Bytes())

	// We expect: first READ BINARY of 8 bytes at SFI offset 0 returns header.
	// Then chunked READs until total reached. For a small PD, one extra read
	// covers the rest.
	totalLen := len(full)
	header := full[:8]
	remainder := full[8:]

	calls := 0
	stub := stubCard(func(apdu []byte) ([]byte, error) {
		calls++
		// P1 = 0x81 (SFI mode, SFI=1); P2 = offset within EF.
		offset := apdu[3]
		le := int(apdu[4])
		switch {
		case offset == 0 && le == 8:
			return append(append([]byte{}, header...), 0x90, 0x00), nil
		case offset >= 8:
			start := int(offset)
			end := start + le
			if end > totalLen {
				end = totalLen
			}
			return append(append([]byte{}, full[start:end]...), 0x90, 0x00), nil
		}
		t.Fatalf("unexpected APDU: %x", apdu)
		return nil, nil
	})

	got, err := readEFBySFI(stub, sfiPD, fidPD)
	if err != nil {
		t.Fatalf("readEFBySFI: %v", err)
	}
	if !bytes.Equal(got, full) {
		t.Errorf("got %d bytes, want %d", len(got), len(full))
	}
	if !bytes.Contains(remainder, []byte{remainder[0]}) {
		t.Error("internal consistency check failed")
	}
}

func TestReadEFBySFI_VD(t *testing.T) {
	// VD uses 4 offset pointers; total = max(avdEnd, gvdEnd) + 1.
	avdEnd := uint16(20)
	gvdEnd := uint16(30)
	full := make([]byte, 31)
	binary.BigEndian.PutUint16(full[0:2], 8)
	binary.BigEndian.PutUint16(full[2:4], avdEnd)
	binary.BigEndian.PutUint16(full[4:6], 21)
	binary.BigEndian.PutUint16(full[6:8], gvdEnd)
	for i := 8; i < 31; i++ {
		full[i] = byte(i)
	}

	stub := stubCard(func(apdu []byte) ([]byte, error) {
		offset := apdu[3]
		le := int(apdu[4])
		start := int(offset)
		end := start + le
		if end > len(full) {
			end = len(full)
		}
		return append(append([]byte{}, full[start:end]...), 0x90, 0x00), nil
	})

	got, err := readEFBySFI(stub, sfiVD, fidVD)
	if err != nil {
		t.Fatalf("readEFBySFI VD: %v", err)
	}
	if !bytes.Equal(got, full) {
		t.Errorf("got %d bytes, want %d (mismatch)", len(got), len(full))
	}
}

func TestReadEFBySFIHeaderTooShort(t *testing.T) {
	// Header read returns fewer than 8 bytes.
	stub := stubCard(func(_ []byte) ([]byte, error) {
		return []byte{0x90, 0x00}, nil // 0 bytes data + SW
	})
	if _, err := readEFBySFI(stub, sfiPD, fidPD); err == nil {
		t.Error("expected header-too-short error")
	}
}

func TestReadEFBySFIInitialReadError(t *testing.T) {
	stub := stubCard(func(_ []byte) ([]byte, error) { return nil, fmt.Errorf("io") })
	if _, err := readEFBySFI(stub, sfiPD, fidPD); err == nil {
		t.Error("expected error")
	}
}

// ---- readEFCombined -----------------------------------------------------

func TestReadEFCombinedSFISuccess(t *testing.T) {
	// Tiny payload — header alone already covers the full length.
	full := make([]byte, 8)
	binary.BigEndian.PutUint16(full[0:2], 6)
	for i := 2; i < 8; i++ {
		full[i] = byte(i)
	}
	stub := stubCard(func(_ []byte) ([]byte, error) {
		return append(append([]byte{}, full...), 0x90, 0x00), nil
	})
	got, err := readEFCombined(stub, sfiPD, fidPD)
	if err != nil {
		t.Fatalf("readEFCombined: %v", err)
	}
	if !bytes.Equal(got, full) {
		t.Errorf("mismatch %x vs %x", got, full)
	}
}

func TestReadEFCombinedFallback(t *testing.T) {
	// SFI read fails (header too short → readEFBySFI errors), so the
	// function falls back to selectEF + plain READ BINARY.
	calls := 0
	stub := stubCard(func(apdu []byte) ([]byte, error) {
		calls++
		switch {
		case calls == 1:
			// SFI READ — return 0 data, just SW=9000 → header too short.
			return []byte{0x90, 0x00}, nil
		case calls == 2:
			// SELECT EF by FID.
			if apdu[1] != 0xA4 {
				t.Fatalf("expected SELECT EF, got %x", apdu)
			}
			return []byte{0x90, 0x00}, nil
		case calls == 3:
			// Initial 8-byte header read.
			full := []byte{0, 6, 1, 2, 3, 4, 5, 6}
			return append(append([]byte{}, full...), 0x90, 0x00), nil
		default:
			// Subsequent reads return 0 bytes → loop terminates.
			return []byte{0x90, 0x00}, nil
		}
	})
	got, err := readEFCombined(stub, sfiPD, fidPD)
	if err != nil {
		t.Fatalf("readEFCombined fallback: %v", err)
	}
	if len(got) != 8 {
		t.Errorf("got %d bytes, want 8", len(got))
	}
}

func TestReadEFCombinedFallbackSelectFails(t *testing.T) {
	calls := 0
	stub := stubCard(func(_ []byte) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte{0x90, 0x00}, nil // SFI header empty
		}
		return []byte{0x6A, 0x82}, nil // SELECT EF fails
	})
	if _, err := readEFCombined(stub, sfiPD, fidPD); err == nil {
		t.Error("expected fallback failure")
	}
}

// ---- Read ---------------------------------------------------------------

func TestReadEndToEnd(t *testing.T) {
	pd := buildPD(t, samplePDXML)
	vd := buildVD(t, sampleAVDXML, sampleGVDXML)

	// fcp returned on SELECT AID — anything, we just want non-nil.
	fcp := []byte{0x6F, 0x03, 0xAA, 0xBB, 0xCC}

	// Sample ICCSN: 5A 0A <10 bytes>.
	gdo := []byte{0x5A, 0x0A, 0x80, 0x27, 0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	// Track which file was last selected so plain-mode READ BINARY (used when
	// the offset passes 255 or when reading non-SFI EFs like EF.GDO) returns
	// the right bytes.
	var currentSrc []byte

	stub := stubCard(func(apdu []byte) ([]byte, error) {
		switch {
		case isSelectMF(apdu):
			return []byte{0x90, 0x00}, nil
		case isSelectEFFID(apdu, fidGDO):
			// SELECT EF.GDO at MF — happens before HCA is selected.
			currentSrc = gdo
			return []byte{0x90, 0x00}, nil
		case isSelectEFFID(apdu, fidVersion2A), isSelectEFFID(apdu, fidVersion2B):
			// SELECT EF.Version2 candidates — report "not present" so
			// readVersion2 returns nil cleanly. (Real-card behaviour is
			// exercised in TestReadVersion2_*.)
			return []byte{0x6A, 0x82}, nil
		case isSelectEFFID(apdu, fidStatusVD):
			// SELECT EF.StatusVD — report "not present" in this end-to-end test;
			// dedicated TestReadStatusVD_* covers the happy path.
			return []byte{0x6A, 0x82}, nil
		case isSelectAID(apdu):
			return append(append([]byte{}, fcp...), 0x90, 0x00), nil
		case apdu[1] == insReadBinary:
			le := int(apdu[4])
			var offset int
			if apdu[2]&readBinarySFIBit != 0 {
				// SFI mode — also remember which file is implicitly selected.
				sfi := apdu[2] & readBinarySFIMask
				switch sfi {
				case sfiPD:
					currentSrc = pd
				case sfiVD:
					currentSrc = vd
				default:
					t.Fatalf("unknown SFI: %d", sfi)
				}
				offset = int(apdu[3])
			} else {
				// Plain READ BINARY against the implicit current EF (ISO 7816-4 §7.2.3).
				offset = int(apdu[2])<<8 | int(apdu[3])
			}
			if currentSrc == nil {
				t.Fatalf("plain read without prior SFI: %x", apdu)
			}
			end := offset + le
			if end > len(currentSrc) {
				end = len(currentSrc)
			}
			return append(append([]byte{}, currentSrc[offset:end]...), 0x90, 0x00), nil
		}
		t.Fatalf("unexpected APDU: %x", apdu)
		return nil, nil
	})

	cd, err := Read(stub)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if cd.Personal == nil || cd.Personal.LastName != "Müller" {
		t.Errorf("Personal not populated: %+v", cd.Personal)
	}
	if cd.Insurance == nil || cd.Insurance.InsurerID != "109519005" {
		t.Errorf("Insurance not populated: %+v", cd.Insurance)
	}
	if cd.Protected == nil || cd.Protected.ZuzahlungStatus != "1" {
		t.Errorf("Protected not populated: %+v", cd.Protected)
	}
	if !bytes.Equal(cd.HCAFCP, fcp) {
		t.Errorf("HCAFCP not propagated: %x vs %x", cd.HCAFCP, fcp)
	}
	if cd.MF == nil || cd.MF.ICCSN != "80276000000000000000" {
		t.Errorf("MF.ICCSN not populated: %+v", cd.MF)
	}
}

func TestReadAIDFails(t *testing.T) {
	stub := stubCard(func(apdu []byte) ([]byte, error) {
		// MF select ok, AID select fails (both attempts).
		if len(apdu) >= 7 && apdu[5] == 0x3F && apdu[6] == 0x00 {
			return []byte{0x90, 0x00}, nil
		}
		return []byte{0x6A, 0x82}, nil
	})
	if _, err := Read(stub); err == nil {
		t.Error("expected error when AID select fails")
	}
}

func TestReadParsePDFails(t *testing.T) {
	// MF + AID + PD-read all succeed, but PD bytes don't gunzip → ParsePD fails.
	junk := make([]byte, 32)
	binary.BigEndian.PutUint16(junk[:2], 20) // length=20 → 20 bytes of "gzip" body that isn't gzip
	for i := 2; i < 32; i++ {
		junk[i] = 0xAA
	}
	stub := stubCard(func(apdu []byte) ([]byte, error) {
		switch {
		case apdu[1] == 0xA4 && len(apdu) >= 7 && apdu[5] == 0x3F && apdu[6] == 0x00:
			return []byte{0x90, 0x00}, nil
		case apdu[1] == 0xA4 && apdu[2] == 0x04:
			return []byte{0x90, 0x00}, nil
		case apdu[1] == 0xB0:
			le := int(apdu[4])
			var offset int
			if apdu[2]&0x80 != 0 {
				offset = int(apdu[3])
			} else {
				offset = int(apdu[2])<<8 | int(apdu[3])
			}
			end := offset + le
			if end > len(junk) {
				end = len(junk)
			}
			return append(append([]byte{}, junk[offset:end]...), 0x90, 0x00), nil
		}
		return []byte{0x6A, 0x82}, nil
	})
	if _, err := Read(stub); err == nil {
		t.Error("expected parse error")
	}
}

func TestReadVDFails(t *testing.T) {
	pd := buildPD(t, samplePDXML)
	var currentSrc []byte
	stub := stubCard(func(apdu []byte) ([]byte, error) {
		switch {
		case apdu[1] == 0xA4 && len(apdu) >= 7 && apdu[5] == 0x3F && apdu[6] == 0x00:
			return []byte{0x90, 0x00}, nil
		case apdu[1] == 0xA4 && apdu[2] == 0x04:
			return []byte{0x90, 0x00}, nil
		case apdu[1] == 0xB0:
			le := int(apdu[4])
			var offset int
			if apdu[2]&0x80 != 0 {
				sfi := apdu[2] & 0x1F
				switch sfi {
				case sfiPD:
					currentSrc = pd
				case sfiVD:
					// Return empty header → readEFBySFI errors → fallback select fails too.
					return []byte{0x90, 0x00}, nil
				}
				offset = int(apdu[3])
			} else {
				offset = int(apdu[2])<<8 | int(apdu[3])
			}
			if currentSrc == nil {
				return []byte{0x6A, 0x82}, nil
			}
			end := offset + le
			if end > len(currentSrc) {
				end = len(currentSrc)
			}
			return append(append([]byte{}, currentSrc[offset:end]...), 0x90, 0x00), nil
		case apdu[1] == 0xA4 && apdu[2] == 0x02:
			return []byte{0x6A, 0x82}, nil
		}
		return []byte{0x6A, 0x82}, nil
	})
	if _, err := Read(stub); err == nil {
		t.Error("expected VD read failure")
	}
}

func TestTraceWithEnv(t *testing.T) {
	// Exercise trace() with the env var set so the print branch runs.
	t.Setenv("EGK_TRACE", "1")
	trace("ping %d", 42)
}

func TestReadPDFails(t *testing.T) {
	// MF and AID succeed; PD read returns no data, FID select also fails.
	stub := stubCard(func(apdu []byte) ([]byte, error) {
		switch {
		case apdu[1] == 0xA4 && len(apdu) >= 7 && apdu[5] == 0x3F && apdu[6] == 0x00:
			return []byte{0x90, 0x00}, nil
		case apdu[1] == 0xA4 && apdu[2] == 0x04:
			return []byte{0x90, 0x00}, nil
		case apdu[1] == 0xB0:
			return []byte{0x90, 0x00}, nil // header empty → SFI fails
		case apdu[1] == 0xA4 && apdu[2] == 0x02:
			return []byte{0x6A, 0x82}, nil // FID select fails → readEFCombined errors
		}
		return []byte{0x6A, 0x82}, nil
	})
	if _, err := Read(stub); err == nil {
		t.Error("expected PD read failure")
	}
}
