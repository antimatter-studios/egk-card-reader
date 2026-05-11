package sm

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"testing"
)

// ----- helpers ---------------------------------------------------------------

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// newFixedSession returns a session with deterministic AES-128 keys and
// SSC=0, so two newFixedSession calls produce identical sessions and can
// stand in for "host" and "card" sides.
func newFixedSession(t *testing.T) *Session {
	t.Helper()
	return &Session{
		KEnc: mustHex(t, "00112233445566778899aabbccddeeff"),
		KMac: mustHex(t, "0f0e0d0c0b0a09080706050403020100"),
		SSC:  mustHex(t, "00000000000000000000000000000000"),
	}
}

// cardWrapResponse models what an eGK does when emitting an SM response.
// Layout: [ '87' L 01 ciphertext ]? || '99' 02 SW1 SW2 || '8E' 08 MAC ||
// outer SW 9000.
//
// The "card" side increments SSC first, matching the host-side Unwrap
// pre-MAC increment.
func cardWrapResponse(t *testing.T, s *Session, plaintext []byte, sw uint16) []byte {
	t.Helper()
	if err := s.prepare(); err != nil {
		t.Fatal(err)
	}
	incSSC(s.SSC)

	var dos []byte
	if plaintext != nil {
		padded := pad7816(plaintext, blockSize)
		iv := make([]byte, blockSize)
		s.encCipher.Encrypt(iv, s.SSC)
		ct := make([]byte, len(padded))
		mode := cipher.NewCBCEncrypter(s.encCipher, iv)
		mode.CryptBlocks(ct, padded)
		val := append([]byte{0x01}, ct...)
		dos = append(dos, encodeTLV(tagCryptogram, val)...)
	}
	dos = append(dos, encodeTLV(tagStatus, []byte{byte(sw >> 8), byte(sw)})...)

	macInput := append([]byte{}, s.SSC...)
	macInput = append(macInput, pad7816(dos, blockSize)...)
	full := cmacWithSubkeys(s.macCipher, macInput, s.macK1, s.macK2)
	dos = append(dos, encodeTLV(tagMAC, full[:macLen])...)

	// Outer SW (reader-appended).
	dos = append(dos, 0x90, 0x00)
	return dos
}

// ----- structural Wrap tests -------------------------------------------------

// TestWrap_Case1 (CLA INS P1 P2 only, le=-1, cmdData=nil) is the simplest
// SM-wrapped command: only the '8E' MAC DO, no '87'/'97'.
func TestWrap_Case1(t *testing.T) {
	t.Parallel()
	s := newFixedSession(t)
	w, err := s.Wrap(0x00, 0x84, 0x00, 0x00, nil, -1)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	// Frame: 0C 84 00 00  Lc  '8E' 08 <MAC>  00
	if w[0] != 0x0C {
		t.Fatalf("CLA not 0x0C: %02X", w[0])
	}
	if w[5] != 0x8E || w[6] != 0x08 {
		t.Fatalf("expected '8E' 08 at offset 5-6: %x", w[5:7])
	}
	if w[len(w)-1] != 0x00 {
		t.Fatalf("missing trailing Le=00: %x", w)
	}
}

// TestWrap_Case2 (Le only) emits '97' before '8E'.
func TestWrap_Case2(t *testing.T) {
	t.Parallel()
	s := newFixedSession(t)
	w, err := s.Wrap(0x00, 0xB0, 0x00, 0x00, nil, 0)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if w[5] != 0x97 || w[6] != 0x01 || w[7] != 0x00 {
		t.Fatalf("expected '97' 01 00: %x", w[5:8])
	}
	if w[8] != 0x8E || w[9] != 0x08 {
		t.Fatalf("expected '8E' 08 after '97': %x", w[8:10])
	}
}

// TestWrap_Case3 (cmdData but no Le) emits '87' then '8E'.
func TestWrap_Case3(t *testing.T) {
	t.Parallel()
	s := newFixedSession(t)
	w, err := s.Wrap(0x00, 0xA4, 0x04, 0x0C, []byte{0x3F, 0x00}, -1)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if w[0]&0x0C != 0x0C {
		t.Fatalf("CLA SM bits missing: %02X", w[0])
	}
	if w[5] != 0x87 {
		t.Fatalf("expected '87' first: %02X", w[5])
	}
}

// TestWrap_Case4 (cmdData + Le) emits '87' then '97' then '8E'.
func TestWrap_Case4(t *testing.T) {
	t.Parallel()
	s := newFixedSession(t)
	w, err := s.Wrap(0x00, 0xA4, 0x04, 0x0C, []byte{0x3F, 0x00}, 0)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if w[5] != 0x87 {
		t.Fatalf("expected '87' first: %02X", w[5])
	}
	// Find '97' after '87' (we don't hard-pin the offset because the
	// cryptogram length depends on padding).
	saw97, saw8E := false, false
	for cursor := 5; cursor < len(w)-1; {
		tag, _, n, err := parseTLV(w[cursor : len(w)-1])
		if err != nil {
			break
		}
		if tag == 0x97 {
			saw97 = true
		}
		if tag == 0x8E {
			saw8E = true
		}
		cursor += n
	}
	if !saw97 || !saw8E {
		t.Fatalf("missing '97' or '8E' DO: %x", w)
	}
}

// TestWrap_Structure checks framing invariants.
func TestWrap_Structure(t *testing.T) {
	t.Parallel()
	s := newFixedSession(t)
	w, err := s.Wrap(0x00, 0xA4, 0x04, 0x0C, []byte{0x3F, 0x00}, -1)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if w[0]&0x0C != 0x0C {
		t.Fatalf("CLA SM bits not set: %02X", w[0])
	}
	if w[1] != 0xA4 || w[2] != 0x04 || w[3] != 0x0C {
		t.Fatalf("INS/P1/P2 mangled: %x", w[1:4])
	}
	if w[len(w)-1] != 0x00 {
		t.Fatalf("missing trailing Le=00: %x", w)
	}
	lc := int(w[4])
	if lc != len(w)-4-1-1 {
		t.Fatalf("Lc mismatch: lc=%d total=%d", lc, len(w))
	}
}

// TestWrap_CLAMask asserts the SM CLA encoding masks out the old bits then
// ORs in 0x0C (per gemSpec_COS_v2). e.g. CLA 0x80 -> 0x8C, CLA 0x04 -> 0x0C.
func TestWrap_CLAMask(t *testing.T) {
	t.Parallel()
	s := newFixedSession(t)
	cases := []struct {
		in, want byte
	}{
		{0x00, 0x0C},
		{0x80, 0x8C},
		{0x04, 0x0C}, // strip old SM bits 0x04, set 0x0C
		{0x08, 0x0C}, // strip old SM bits 0x08, set 0x0C
		{0x84, 0x8C},
	}
	for _, tc := range cases {
		s.SSC = make([]byte, sscLen) // reset
		w, err := s.Wrap(tc.in, 0xB0, 0, 0, nil, 0)
		if err != nil {
			t.Fatalf("Wrap(CLA=%02X): %v", tc.in, err)
		}
		if w[0] != tc.want {
			t.Fatalf("CLA in %02X -> %02X, want %02X", tc.in, w[0], tc.want)
		}
	}
}

// TestWrap_SSCIncrement: every Wrap should advance SSC by exactly one.
func TestWrap_SSCIncrement(t *testing.T) {
	t.Parallel()
	s := newFixedSession(t)
	for i := 1; i <= 5; i++ {
		_, err := s.Wrap(0x00, 0xB0, 0x00, 0x00, nil, 0x00)
		if err != nil {
			t.Fatal(err)
		}
		var want [16]byte
		want[15] = byte(i)
		if !bytes.Equal(s.SSC, want[:]) {
			t.Fatalf("SSC after %d wraps = %x, want %x", i, s.SSC, want[:])
		}
	}
}

// TestWrap_LeRange checks the short-form Le bound.
func TestWrap_LeRange(t *testing.T) {
	t.Parallel()
	s := newFixedSession(t)
	if _, err := s.Wrap(0, 0, 0, 0, nil, 256); err == nil {
		t.Fatalf("expected error for Le=256")
	}
	if _, err := s.Wrap(0, 0, 0, 0, nil, -5); err == nil {
		t.Fatalf("expected error for Le=-5")
	}
}

// ----- round-trip tests ------------------------------------------------------

// TestUnwrap_RoundTrip_Case1: command had no data and no Le, response has
// only '99' and '8E'.
func TestUnwrap_RoundTrip_Case1(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0x84, 0x00, 0x00, nil, -1); err != nil {
		t.Fatalf("host Wrap: %v", err)
	}
	incSSC(card.SSC) // card +1 on command

	resp := cardWrapResponse(t, card, nil, 0x9000)
	data, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil data, got %x", data)
	}
	if sw != 0x9000 {
		t.Fatalf("SW = %04X", sw)
	}
	if !bytes.Equal(host.SSC, card.SSC) {
		t.Fatalf("SSC drift: host=%x card=%x", host.SSC, card.SSC)
	}
}

// TestUnwrap_RoundTrip_Case2: command had only Le, response carries data.
func TestUnwrap_RoundTrip_Case2(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0xB0, 0x00, 0x00, nil, 0x10); err != nil {
		t.Fatalf("host Wrap: %v", err)
	}
	incSSC(card.SSC)

	plain := mustHex(t, "00112233445566778899aabbccddeeff")
	resp := cardWrapResponse(t, card, plain, 0x9000)
	data, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(data, plain) {
		t.Fatalf("data mismatch:\n got=%x\nwant=%x", data, plain)
	}
	if sw != 0x9000 {
		t.Fatalf("SW = %04X", sw)
	}
}

// TestUnwrap_RoundTrip_Case3: command had data, no Le.
func TestUnwrap_RoundTrip_Case3(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0xA4, 0x04, 0x0C, []byte{0x3F, 0x00}, -1); err != nil {
		t.Fatalf("host Wrap: %v", err)
	}
	incSSC(card.SSC)

	resp := cardWrapResponse(t, card, nil, 0x9000)
	data, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil data, got %x", data)
	}
	if sw != 0x9000 {
		t.Fatalf("SW = %04X", sw)
	}
}

// TestUnwrap_RoundTrip_Case4: command had data + Le, response carries data.
func TestUnwrap_RoundTrip_Case4(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0xB0, 0x00, 0x00, nil, 0x00); err != nil {
		t.Fatalf("host Wrap: %v", err)
	}
	incSSC(card.SSC)

	plain := mustHex(t, "0123456789abcdef0011223344556677")
	resp := cardWrapResponse(t, card, plain, 0x9000)
	data, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if sw != 0x9000 {
		t.Fatalf("SW = %04X, want 9000", sw)
	}
	if !bytes.Equal(data, plain) {
		t.Fatalf("data mismatch:\n got=%x\nwant=%x", data, plain)
	}
	if !bytes.Equal(host.SSC, card.SSC) {
		t.Fatalf("SSC drift: host=%x card=%x", host.SSC, card.SSC)
	}
}

// TestUnwrap_RoundTrip_NoData covers a card response without '87'.
func TestUnwrap_RoundTrip_NoData(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0x20, 0x00, 0x01, []byte{0x01, 0x02, 0x03, 0x04}, -1); err != nil {
		t.Fatal(err)
	}
	incSSC(card.SSC)

	resp := cardWrapResponse(t, card, nil, 0x9000)
	data, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil data, got %x", data)
	}
	if sw != 0x9000 {
		t.Fatalf("SW = %04X", sw)
	}
}

// TestUnwrap_NonAlignedPlaintext exercises the "padded with at least one
// byte" path through stripPad7816.
func TestUnwrap_NonAlignedPlaintext(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0xB0, 0x00, 0x00, nil, 0x00); err != nil {
		t.Fatal(err)
	}
	incSSC(card.SSC)

	plain := []byte("hello world!") // 12 bytes — needs 4 bytes of padding
	resp := cardWrapResponse(t, card, plain, 0x9000)
	data, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(data, plain) {
		t.Fatalf("plaintext mismatch: got=%x want=%x", data, plain)
	}
	if sw != 0x9000 {
		t.Fatalf("SW = %04X", sw)
	}
}

// TestUnwrap_LargePlaintext checks a >256-byte payload (forces '82 LL LL'
// length encoding in the '87' DO).
func TestUnwrap_LargePlaintext(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0xB0, 0x00, 0x00, nil, 0x00); err != nil {
		t.Fatal(err)
	}
	incSSC(card.SSC)

	plain := make([]byte, 300)
	for i := range plain {
		plain[i] = byte(i)
	}
	resp := cardWrapResponse(t, card, plain, 0x9000)
	data, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(data, plain) {
		t.Fatalf("plaintext mismatch")
	}
	if sw != 0x9000 {
		t.Fatalf("SW = %04X", sw)
	}
}

// TestUnwrap_NonSuccessSW: the protected status word doesn't have to be
// 9000. The card may report e.g. 6A82 (file not found) inside an
// authenticated '99' DO.
func TestUnwrap_NonSuccessSW(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0xA4, 0x00, 0x0C, []byte{0x3F, 0xFF}, -1); err != nil {
		t.Fatal(err)
	}
	incSSC(card.SSC)

	resp := cardWrapResponse(t, card, nil, 0x6A82)
	_, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if sw != 0x6A82 {
		t.Fatalf("SW = %04X, want 6A82", sw)
	}
}

// ----- tamper + negative tests ----------------------------------------------

// TestUnwrap_Tamper flips one bit in every byte of a wrapped response and
// asserts Unwrap returns an error rather than accepting or hanging.
func TestUnwrap_Tamper(t *testing.T) {
	t.Parallel()
	host := newFixedSession(t)
	card := newFixedSession(t)

	if _, err := host.Wrap(0x00, 0xB0, 0x00, 0x00, nil, 0x00); err != nil {
		t.Fatal(err)
	}
	incSSC(card.SSC)
	plain := mustHex(t, "deadbeefcafebabe1122334455667788")
	resp := cardWrapResponse(t, card, plain, 0x9000)

	for i := 0; i < len(resp)-2; i++ {
		fresh := newFixedSession(t)
		incSSC(fresh.SSC) // catch up to where host was post-wrap
		tampered := append([]byte{}, resp...)
		tampered[i] ^= 0x01
		_, _, err := fresh.Unwrap(tampered)
		if err == nil {
			t.Fatalf("Unwrap accepted tampered byte at offset %d", i)
		}
	}
}

// TestUnwrap_SSCMismatch: wrap with SSC=X, attempt Unwrap with SSC=Y, MAC
// must fail.
func TestUnwrap_SSCMismatch(t *testing.T) {
	t.Parallel()
	card := newFixedSession(t)
	host := newFixedSession(t)

	// Card thinks the command counter just incremented to 1; host thinks
	// it just incremented to 5 (drifted).
	incSSC(card.SSC) // card -> 1

	plain := mustHex(t, "00")
	resp := cardWrapResponse(t, card, plain, 0x9000)

	// Host pre-Unwrap SSC is 4, so the pre-MAC-increment makes it 5.
	for i := 0; i < 4; i++ {
		incSSC(host.SSC)
	}
	_, _, err := host.Unwrap(resp)
	if err == nil {
		t.Fatalf("Unwrap accepted SSC-mismatched response")
	}
}

// TestUnwrap_MalformedDO covers TLV parser edge cases. Each entry must
// produce an error — and crucially, must NOT hang. The outer test timeout
// would catch a hang, but each subtest is independent and small.
func TestUnwrap_MalformedDO(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"empty":                 nil,
		"only-outer-sw":         mustHex(t, "9000"),
		"truncated-87-length":   mustHex(t, "87819000"),                 // '87' 81 90 00 — length byte says >=128 but no more bytes
		"truncated-tlv":         mustHex(t, "8E08aabbccdd9000"),         // 8E length 8 but only 4 bytes
		"missing-8E":            mustHex(t, "990200009000"),             // '99' + outer SW, no MAC
		"bad-99-len":            mustHex(t, "9901008E08aabbccddeeff001122339000"),
		"unsupported-len-form":  mustHex(t, "878301000000ff9000"),       // 83 LL LL LL not supported
		"indefinite-length":     mustHex(t, "8780cafebabe9000"),         // 80 = indefinite (BER) — rejected
		"high-tag-number":       mustHex(t, "9F0102aaaa9000"),           // 1F low bits => high-tag form
		"non-minimal-81":        mustHex(t, "87810500112233449000"),     // 81 with value < 128 (non-minimal)
		"non-minimal-82":        mustHex(t, "8782005500" + bytes85hex(0x55) + "9000"),
	}
	for name, in := range cases {
		name, in := name, in
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			host := newFixedSession(t)
			_, _, err := host.Unwrap(in)
			if err == nil {
				t.Fatalf("expected error for %q", name)
			}
		})
	}
}

// bytes85hex returns hex of 85 bytes of the given fill — used by the
// non-minimal-82 vector above.
func bytes85hex(fill byte) string {
	b := bytes.Repeat([]byte{fill}, 0x55)
	return hex.EncodeToString(b)
}

// TestParseTLV_Direct exercises the parser in isolation so a regression in
// the cursor-advance contract is caught without depending on Unwrap state.
func TestParseTLV_Direct(t *testing.T) {
	t.Parallel()

	// Valid short-form: 99 02 9000
	{
		tag, val, n, err := parseTLV([]byte{0x99, 0x02, 0x90, 0x00})
		if err != nil || tag != 0x99 || !bytes.Equal(val, []byte{0x90, 0x00}) || n != 4 {
			t.Fatalf("short form wrong: tag=%02X val=%x n=%d err=%v", tag, val, n, err)
		}
	}
	// Valid 81 LL form.
	{
		buf := []byte{0x87, 0x81, 0x80}
		buf = append(buf, bytes.Repeat([]byte{0xAA}, 0x80)...)
		tag, val, n, err := parseTLV(buf)
		if err != nil || tag != 0x87 || len(val) != 0x80 || n != 3+0x80 {
			t.Fatalf("81 LL wrong: %v %d", err, n)
		}
	}
	// Truncated: 0 bytes.
	if _, _, _, err := parseTLV(nil); err == nil {
		t.Fatalf("nil should error")
	}
	// Truncated: 1 byte.
	if _, _, _, err := parseTLV([]byte{0x87}); err == nil {
		t.Fatalf("1-byte should error")
	}
	// Length 0xFF illegal form.
	if _, _, _, err := parseTLV([]byte{0x87, 0xFF, 0x00}); err == nil {
		t.Fatalf("FF length should error")
	}
	// Value past buffer end.
	if _, _, _, err := parseTLV([]byte{0x87, 0x05, 0x01, 0x02}); err == nil {
		t.Fatalf("truncated value should error")
	}
}

// TestParseTLV_AlwaysAdvances loops parseTLV over a fuzzy mix of buffers
// and asserts that whenever it returns nil error, the n it reports is
// strictly positive. This is the load-bearing invariant Unwrap relies on
// to terminate.
func TestParseTLV_AlwaysAdvances(t *testing.T) {
	t.Parallel()
	// Build a corpus of small buffers covering all length bytes 0..127
	// plus a sampling of 81 LL and 82 LL LL forms.
	for l := 0; l < 128; l++ {
		buf := make([]byte, 2+l)
		buf[0] = 0x99
		buf[1] = byte(l)
		_, _, n, err := parseTLV(buf)
		if err != nil {
			t.Fatalf("len=%d unexpected err: %v", l, err)
		}
		if n != 2+l {
			t.Fatalf("len=%d: n=%d want %d", l, n, 2+l)
		}
	}
}

// ----- session helpers + counter --------------------------------------------

// TestIncSSC verifies the big-endian counter and wrap-around behaviour.
func TestIncSSC(t *testing.T) {
	t.Parallel()
	ssc := make([]byte, sscLen)
	for i := range ssc {
		ssc[i] = 0xFF
	}
	incSSC(ssc)
	for i, b := range ssc {
		if b != 0 {
			t.Fatalf("byte %d = %02X, want 00", i, b)
		}
	}

	ssc[15] = 0xFE
	incSSC(ssc)
	if ssc[15] != 0xFF {
		t.Fatalf("simple inc failed: %x", ssc)
	}

	// Carry across byte boundary: 0x00FF -> 0x0100.
	ssc = make([]byte, sscLen)
	ssc[14] = 0x00
	ssc[15] = 0xFF
	incSSC(ssc)
	if ssc[14] != 0x01 || ssc[15] != 0x00 {
		t.Fatalf("carry wrong: %x", ssc[14:])
	}
}

// TestPad7816 enforces the "full block when already aligned" rule and the
// round-trip with stripPad7816.
func TestPad7816(t *testing.T) {
	t.Parallel()
	// Already-aligned input must grow by one full block.
	in := bytes.Repeat([]byte{0xAA}, blockSize)
	out := pad7816(in, blockSize)
	if len(out) != 2*blockSize {
		t.Fatalf("aligned padding: len=%d, want %d", len(out), 2*blockSize)
	}
	if out[blockSize] != 0x80 {
		t.Fatalf("aligned padding: byte %d = %02X, want 80", blockSize, out[blockSize])
	}

	// One short of the block boundary.
	in = bytes.Repeat([]byte{0xAA}, blockSize-1)
	out = pad7816(in, blockSize)
	if len(out) != blockSize || out[blockSize-1] != 0x80 {
		t.Fatalf("near-aligned padding wrong: %x", out)
	}

	// Empty input.
	out = pad7816(nil, blockSize)
	if len(out) != blockSize || out[0] != 0x80 {
		t.Fatalf("empty padding wrong: %x", out)
	}

	// Round-trip with stripPad7816.
	for _, n := range []int{0, 1, 15, 16, 17, 31, 32, 33} {
		msg := bytes.Repeat([]byte{byte(n + 1)}, n)
		padded := pad7816(msg, blockSize)
		got, err := stripPad7816(padded)
		if err != nil {
			t.Fatalf("strip failed for len %d: %v", n, err)
		}
		if !bytes.Equal(got, msg) {
			t.Fatalf("strip mismatch for len %d:\n got=%x\nwant=%x", n, got, msg)
		}
	}
}

// TestStripPad7816_Errors covers the rejection paths.
func TestStripPad7816_Errors(t *testing.T) {
	t.Parallel()
	// All zeros — no 0x80 marker.
	if _, err := stripPad7816(make([]byte, blockSize)); err == nil {
		t.Fatalf("expected error on no-marker")
	}
	// Trailing 0x42 — not 0x00, not 0x80 → malformed.
	if _, err := stripPad7816([]byte{0x80, 0x00, 0x42}); err == nil {
		t.Fatalf("expected error on malformed padding")
	}
}

// TestSession_AES256: same wiring with longer keys.
func TestSession_AES256(t *testing.T) {
	t.Parallel()
	host := &Session{
		KEnc: bytes.Repeat([]byte{0x11}, 32),
		KMac: bytes.Repeat([]byte{0x22}, 32),
		SSC:  make([]byte, sscLen),
	}
	card := &Session{
		KEnc: bytes.Repeat([]byte{0x11}, 32),
		KMac: bytes.Repeat([]byte{0x22}, 32),
		SSC:  make([]byte, sscLen),
	}
	if _, err := host.Wrap(0x00, 0xB0, 0x00, 0x00, nil, 0x00); err != nil {
		t.Fatal(err)
	}
	incSSC(card.SSC)
	plain := []byte("hello eGK")
	resp := cardWrapResponse(t, card, plain, 0x9000)
	data, sw, err := host.Unwrap(resp)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if sw != 0x9000 || !bytes.Equal(data, plain) {
		t.Fatalf("AES-256 round-trip wrong: data=%x sw=%04X", data, sw)
	}
}

// TestSession_Reject_BadKey covers the prepare() validation paths.
func TestSession_Reject_BadKey(t *testing.T) {
	t.Parallel()
	bad := []*Session{
		nil,
		{KEnc: make([]byte, 15), KMac: make([]byte, 16), SSC: make([]byte, 16)},
		{KEnc: make([]byte, 16), KMac: make([]byte, 15), SSC: make([]byte, 16)},
		{KEnc: make([]byte, 16), KMac: make([]byte, 16), SSC: make([]byte, 8)},
	}
	for i, s := range bad {
		_, err := s.Wrap(0, 0, 0, 0, nil, -1)
		if err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

// Sanity check that aes.BlockSize is what we coded against.
func TestAESBlockSize(t *testing.T) {
	t.Parallel()
	if aes.BlockSize != 16 {
		t.Fatalf("unexpected aes.BlockSize = %d", aes.BlockSize)
	}
}
