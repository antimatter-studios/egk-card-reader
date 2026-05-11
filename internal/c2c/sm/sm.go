package sm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
)

// AES block size, the only cipher gemSpec_COS uses for SM today.
const blockSize = aes.BlockSize // 16

// Truncated CMAC length for the '8E' data object.
const macLen = 8

// Send-Sequence-Counter length for AES SM (one AES block).
const sscLen = 16

// Tag constants from ISO 7816-4 / gemSpec_COS.
const (
	tagCryptogram = 0x87 // padding-content indicator || ciphertext
	tagLe         = 0x97 // expected response length (command direction)
	tagStatus     = 0x99 // protected status word (response direction)
	tagMAC        = 0x8E // cryptographic checksum
)

// Bitmask flags used by Unwrap to detect duplicate DOs in O(1) without
// relying on nil vs. empty distinctions (an "87 00" cryptogram has an
// empty value slice but must still count as "seen").
const (
	seen87 uint8 = 1 << 0
	seen99 uint8 = 1 << 1
	seen8E uint8 = 1 << 2
)

// Session bundles the symmetric keys and Send-Sequence-Counter (SSC) for
// one secure-messaging session.
//
//	K_ENC, K_MAC : AES-128 (16 bytes) or AES-256 (32 bytes).
//	SSC          : exactly 16 bytes (one AES block), big-endian counter.
//
// SSC is mutated in place. Every Wrap and every Unwrap increments it by
// one before computing/verifying the MAC, matching the on-card behaviour
// in gemSpec_COS §10.
type Session struct {
	KEnc []byte
	KMac []byte
	SSC  []byte

	// Cached cipher.Block instances and CMAC subkeys. Lazily derived on
	// first prepare(); rebuilt if KMac changes length (a sloppy way to
	// detect a re-keyed session, sufficient for tests).
	encCipher cipher.Block
	macCipher cipher.Block
	macK1     []byte
	macK2     []byte
	cachedFor int // len(KMac) the cached subkeys belong to
}

// prepare validates the Session and builds cached ciphers + subkeys.
func (s *Session) prepare() error {
	if s == nil {
		return errors.New("sm: nil session")
	}
	if len(s.SSC) != sscLen {
		return fmt.Errorf("sm: SSC must be %d bytes, have %d", sscLen, len(s.SSC))
	}
	if l := len(s.KEnc); l != 16 && l != 24 && l != 32 {
		return errShortKey
	}
	if l := len(s.KMac); l != 16 && l != 24 && l != 32 {
		return errShortKey
	}
	if s.encCipher == nil {
		c, err := aes.NewCipher(s.KEnc)
		if err != nil {
			return err
		}
		s.encCipher = c
	}
	if s.macCipher == nil || s.cachedFor != len(s.KMac) {
		c, err := aes.NewCipher(s.KMac)
		if err != nil {
			return err
		}
		s.macCipher = c
		s.macK1, s.macK2 = cmacSubkeys(c)
		s.cachedFor = len(s.KMac)
	}
	return nil
}

// incSSC increments the SSC in place, big-endian, by one. Wraps at 2^128.
func incSSC(ssc []byte) {
	for i := len(ssc) - 1; i >= 0; i-- {
		ssc[i]++
		if ssc[i] != 0 {
			return
		}
	}
}

// pad7816 returns m padded with ISO/IEC 7816-4 padding mode 2 (append 0x80,
// then 0x00s) to a multiple of bs. If the input is already block-aligned,
// a *full* extra block is appended (mandatory per the spec).
func pad7816(m []byte, bs int) []byte {
	padLen := bs - (len(m) % bs)
	if padLen == 0 {
		padLen = bs
	}
	out := make([]byte, len(m)+padLen)
	copy(out, m)
	out[len(m)] = 0x80
	return out
}

// stripPad7816 removes ISO/IEC 7816-4 padding mode 2. Errors on malformed
// or absent padding.
func stripPad7816(m []byte) ([]byte, error) {
	for i := len(m) - 1; i >= 0; i-- {
		switch m[i] {
		case 0x00:
			continue
		case 0x80:
			return m[:i], nil
		default:
			return nil, errors.New("sm: malformed ISO 7816-4 padding")
		}
	}
	return nil, errors.New("sm: missing ISO 7816-4 padding")
}

// cbcEncrypt returns AES-CBC(K_ENC, IV, m). m must already be padded to a
// multiple of the block size. The IV is AES_K_ENC(SSC) — masking the very
// regular leading bytes of the counter per gemSpec_COS.
func (s *Session) cbcEncrypt(m []byte) ([]byte, error) {
	if len(m)%blockSize != 0 {
		return nil, errors.New("sm: plaintext length is not a multiple of block size")
	}
	iv := make([]byte, blockSize)
	s.encCipher.Encrypt(iv, s.SSC)
	out := make([]byte, len(m))
	mode := cipher.NewCBCEncrypter(s.encCipher, iv)
	mode.CryptBlocks(out, m)
	return out, nil
}

// cbcDecrypt is the inverse of cbcEncrypt.
func (s *Session) cbcDecrypt(c []byte) ([]byte, error) {
	if len(c) == 0 || len(c)%blockSize != 0 {
		return nil, errors.New("sm: ciphertext length is not a positive multiple of block size")
	}
	iv := make([]byte, blockSize)
	s.encCipher.Encrypt(iv, s.SSC)
	out := make([]byte, len(c))
	mode := cipher.NewCBCDecrypter(s.encCipher, iv)
	mode.CryptBlocks(out, c)
	return out, nil
}

// encodeTLV writes a BER-TLV triple with a single-byte tag using the
// shortest legal length encoding. Values larger than 65535 bytes are not
// produced by C2C SM but the function still emits a valid encoding.
func encodeTLV(tag byte, value []byte) []byte {
	var hdr []byte
	switch {
	case len(value) < 0x80:
		hdr = []byte{tag, byte(len(value))}
	case len(value) < 0x100:
		hdr = []byte{tag, 0x81, byte(len(value))}
	case len(value) < 0x10000:
		hdr = []byte{tag, 0x82, byte(len(value) >> 8), byte(len(value))}
	default:
		hdr = []byte{tag, 0x84,
			byte(len(value) >> 24), byte(len(value) >> 16),
			byte(len(value) >> 8), byte(len(value))}
	}
	out := make([]byte, 0, len(hdr)+len(value))
	out = append(out, hdr...)
	out = append(out, value...)
	return out
}

// parseTLV decodes one BER-TLV triple with a single-byte tag from buf. It
// returns the tag, the value bytes (a sub-slice of buf), and the number of
// bytes consumed (tag + length-encoding + value).
//
// Indefinite-length form (0x80) is rejected — gemSpec_COS only uses
// definite lengths. Multi-byte tags (high tag-number form) are rejected.
//
// The function GUARANTEES that on success the returned consumed count is
// at least 2 and at most len(buf). Callers can therefore safely advance
// the cursor by the returned count without risking an infinite loop.
func parseTLV(buf []byte) (tag byte, value []byte, n int, err error) {
	if len(buf) < 2 {
		return 0, nil, 0, errors.New("sm: TLV truncated (need tag+length)")
	}
	tag = buf[0]
	// Reject high-tag-number form. Single-byte tag means low 5 bits != 0x1F
	// for a BER tag — gemSpec_COS SM tags ('87', '97', '99', '8E') are all
	// single-byte primitives, so any value with the low 5 bits == 0x1F is
	// either an unknown multi-byte tag or a malformed length byte
	// masquerading as a tag.
	if tag&0x1F == 0x1F {
		return 0, nil, 0, fmt.Errorf("sm: high-tag-number form not supported (tag start 0x%02X)", tag)
	}

	lenByte := buf[1]
	var (
		l   int
		off int
	)
	switch {
	case lenByte < 0x80:
		l = int(lenByte)
		off = 2
	case lenByte == 0x80:
		// Indefinite length — illegal here.
		return 0, nil, 0, errors.New("sm: indefinite-length TLV form not supported")
	case lenByte == 0x81:
		if len(buf) < 3 {
			return 0, nil, 0, errors.New("sm: TLV truncated (81 LL)")
		}
		l = int(buf[2])
		off = 3
		if l < 0x80 {
			return 0, nil, 0, errors.New("sm: non-minimal TLV length encoding (81 LL with LL<128)")
		}
	case lenByte == 0x82:
		if len(buf) < 4 {
			return 0, nil, 0, errors.New("sm: TLV truncated (82 LL LL)")
		}
		l = int(buf[2])<<8 | int(buf[3])
		off = 4
		if l < 0x100 {
			return 0, nil, 0, errors.New("sm: non-minimal TLV length encoding (82 LL LL with value<256)")
		}
	default:
		// 0x83..0xFF: longer forms — not produced by gemSpec_COS SM.
		return 0, nil, 0, fmt.Errorf("sm: unsupported TLV length form 0x%02X", lenByte)
	}

	if l < 0 {
		return 0, nil, 0, errors.New("sm: negative TLV length")
	}
	consumed := off + l
	if consumed > len(buf) {
		return 0, nil, 0, errors.New("sm: TLV value truncated")
	}
	if consumed < 2 {
		// Defensive: should be unreachable given the branches above.
		return 0, nil, 0, errors.New("sm: TLV consumed 0 bytes")
	}
	return tag, buf[off:consumed], consumed, nil
}

// Wrap protects a plain ISO 7816-4 APDU per gemSpec_COS §10.
//
// cmdData == nil → case 1 (no data) or case 2 (Le only, le >= 0).
// cmdData != nil → case 3 (no Le, le == -1) or case 4 (le >= 0).
//
// Le must be in [-1, 255]; short-form encoding only. The SM CLA bits 0x0C
// are forced on. SSC is incremented in place BEFORE MAC computation.
func (s *Session) Wrap(cla, ins, p1, p2 byte, cmdData []byte, le int) ([]byte, error) {
	if err := s.prepare(); err != nil {
		return nil, err
	}
	if le < -1 || le > 0xFF {
		return nil, fmt.Errorf("sm: Le %d out of range for short form", le)
	}

	// 1. Build the protected header. Force SM bits 0x0C in CLA per
	//    gemSpec_COS_v2 ("SM with header authentication, ISO formatting").
	protCLA := (cla & 0xFC) | 0x0C
	header := []byte{protCLA, ins, p1, p2}

	// 2. Increment SSC *before* MAC computation. The card increments the
	//    same way on receipt.
	incSSC(s.SSC)

	// 3. Build the data objects that go into the body (everything except
	//    the '8E' MAC, which we append last after the MAC is known).
	var bodyDOs []byte
	if len(cmdData) > 0 {
		padded := pad7816(cmdData, blockSize)
		ct, err := s.cbcEncrypt(padded)
		if err != nil {
			return nil, err
		}
		// 0x01 leading byte = "ISO 7816-4 padding mode 2 was applied".
		cryptogram := make([]byte, 1+len(ct))
		cryptogram[0] = 0x01
		copy(cryptogram[1:], ct)
		bodyDOs = append(bodyDOs, encodeTLV(tagCryptogram, cryptogram)...)
	}
	if le >= 0 {
		bodyDOs = append(bodyDOs, encodeTLV(tagLe, []byte{byte(le)})...)
	}

	// 4. MAC input: SSC || pad(header) || pad(bodyDOs). gemSpec_COS_v2
	//    treats the header pad as a separate block and the DOs as a
	//    separate (padded) section.
	macInput := make([]byte, 0, sscLen+blockSize+len(bodyDOs)+blockSize)
	macInput = append(macInput, s.SSC...)
	macInput = append(macInput, pad7816(header, blockSize)...)
	if len(bodyDOs) > 0 {
		macInput = append(macInput, pad7816(bodyDOs, blockSize)...)
	}
	full := cmacWithSubkeys(s.macCipher, macInput, s.macK1, s.macK2)
	mac := full[:macLen]

	// 5. Compose the final body: bodyDOs || '8E' 08 MAC.
	body := make([]byte, 0, len(bodyDOs)+2+macLen)
	body = append(body, bodyDOs...)
	body = append(body, encodeTLV(tagMAC, mac)...)

	// 6. Frame the APDU. A trailing Le=0x00 is always sent on SM-protected
	//    commands (the card uses it to flag that the response may carry
	//    SM DOs).
	if len(body) > 0xFF {
		// Extended-length wrapper. gemSpec_COS C2C never produces bodies
		// this large; this branch exists for completeness.
		out := make([]byte, 0, 4+3+len(body)+2)
		out = append(out, header...)
		out = append(out, 0x00, byte(len(body)>>8), byte(len(body)))
		out = append(out, body...)
		out = append(out, 0x00, 0x00)
		return out, nil
	}
	out := make([]byte, 0, 4+1+len(body)+1)
	out = append(out, header...)
	out = append(out, byte(len(body)))
	out = append(out, body...)
	out = append(out, 0x00)
	return out, nil
}

// Unwrap inverses Wrap on a response. The input may optionally include the
// trailing two-byte status word that the reader appends — we strip it if
// present, but the authoritative status word is the one carried inside the
// '99' DO.
//
// SSC is incremented in place BEFORE MAC verification, matching Wrap.
func (s *Session) Unwrap(resp []byte) (data []byte, sw uint16, err error) {
	if err := s.prepare(); err != nil {
		return nil, 0, err
	}
	if len(resp) < 2 {
		return nil, 0, errors.New("sm: response too short")
	}

	// Strip the outer SW1-SW2 (reader-appended) if present.
	body := resp[:len(resp)-2]

	// Parse all DOs in order. Track presence in a bitmask so empty values
	// (e.g. "87 00") still count as "seen" without falsely tripping the
	// nil-vs-empty duplicate check.
	var (
		seen          uint8
		cryptogram    []byte // value of '87' DO (nil if not present)
		statusVal     []byte
		macVal        []byte
		macScopeDOs   []byte // raw encoding of every DO except '8E'
	)

	// Defensive cursor: advance by exactly the number of bytes parseTLV
	// reports consumed; require it to be > 0 and within bounds; otherwise
	// bail out with an error. This guarantees termination.
	for cursor := 0; cursor < len(body); {
		tag, val, n, perr := parseTLV(body[cursor:])
		if perr != nil {
			return nil, 0, perr
		}
		if n <= 0 || cursor+n > len(body) {
			// parseTLV is supposed to guarantee progress, but be defensive
			// against future refactors.
			return nil, 0, errors.New("sm: TLV parser failed to advance")
		}
		raw := body[cursor : cursor+n]
		cursor += n

		switch tag {
		case tagCryptogram:
			if seen&seen87 != 0 {
				return nil, 0, errors.New("sm: duplicate '87' DO")
			}
			seen |= seen87
			cryptogram = val
			macScopeDOs = append(macScopeDOs, raw...)
		case tagStatus:
			if seen&seen99 != 0 {
				return nil, 0, errors.New("sm: duplicate '99' DO")
			}
			seen |= seen99
			if len(val) != 2 {
				return nil, 0, fmt.Errorf("sm: '99' DO has length %d, want 2", len(val))
			}
			statusVal = val
			macScopeDOs = append(macScopeDOs, raw...)
		case tagMAC:
			if seen&seen8E != 0 {
				return nil, 0, errors.New("sm: duplicate '8E' DO")
			}
			seen |= seen8E
			if len(val) != macLen {
				return nil, 0, fmt.Errorf("sm: '8E' DO has length %d, want %d", len(val), macLen)
			}
			macVal = val
		default:
			// Unknown SM DOs are still part of the MAC scope.
			macScopeDOs = append(macScopeDOs, raw...)
		}
	}

	if seen&seen8E == 0 {
		return nil, 0, errors.New("sm: missing '8E' DO")
	}
	if seen&seen99 == 0 {
		return nil, 0, errors.New("sm: missing '99' DO")
	}

	// Increment SSC before MAC verification, matching Wrap and the card.
	incSSC(s.SSC)

	macInput := make([]byte, 0, sscLen+len(macScopeDOs)+blockSize)
	macInput = append(macInput, s.SSC...)
	if len(macScopeDOs) > 0 {
		macInput = append(macInput, pad7816(macScopeDOs, blockSize)...)
	}
	full := cmacWithSubkeys(s.macCipher, macInput, s.macK1, s.macK2)
	if subtle.ConstantTimeCompare(full[:macLen], macVal) != 1 {
		return nil, 0, errors.New("sm: MAC verification failed")
	}

	sw = binary.BigEndian.Uint16(statusVal)

	if seen&seen87 != 0 {
		// '87' was present. Reject an empty cryptogram (no padding
		// indicator) or one with an unsupported indicator.
		if len(cryptogram) < 1 {
			return nil, 0, errors.New("sm: empty '87' cryptogram")
		}
		if cryptogram[0] != 0x01 {
			return nil, 0, fmt.Errorf("sm: unsupported padding-content indicator 0x%02X in '87'", cryptogram[0])
		}
		if len(cryptogram) == 1 {
			// Indicator only, no ciphertext. Treat as empty data.
			return nil, sw, nil
		}
		pt, derr := s.cbcDecrypt(cryptogram[1:])
		if derr != nil {
			return nil, 0, derr
		}
		data, derr = stripPad7816(pt)
		if derr != nil {
			return nil, 0, derr
		}
	}

	return data, sw, nil
}
