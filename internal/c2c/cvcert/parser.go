package cvcert

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// KeyAlg enumerates the public-key algorithms we recognise in CV-certificates.
type KeyAlg int

const (
	// AlgUnknown is the zero value; never returned by a successful Parse.
	AlgUnknown KeyAlg = 0
	// AlgRSA2048 is RSA-2048 with PKCS#1 v1.5 padding and SHA-256.
	AlgRSA2048 KeyAlg = 1
	// AlgBrainpoolP256r1 is ECDSA over Brainpool P256r1 with SHA-256.
	AlgBrainpoolP256r1 KeyAlg = 2
	// AlgBrainpoolP384r1 is ECDSA over Brainpool P384r1 with SHA-384.
	AlgBrainpoolP384r1 KeyAlg = 3
	// AlgBrainpoolP512r1 is ECDSA over Brainpool P512r1 with SHA-512.
	AlgBrainpoolP512r1 KeyAlg = 4
)

// String returns a short human-readable name for the algorithm.
func (a KeyAlg) String() string {
	switch a {
	case AlgRSA2048:
		return "RSA-2048-PKCS1v1_5-SHA256"
	case AlgBrainpoolP256r1:
		return "ECDSA-BrainpoolP256r1-SHA256"
	case AlgBrainpoolP384r1:
		return "ECDSA-BrainpoolP384r1-SHA384"
	case AlgBrainpoolP512r1:
		return "ECDSA-BrainpoolP512r1-SHA512"
	default:
		return "Unknown"
	}
}

// RSAPublicKey is an RSA public key extracted from tag 7F49.
type RSAPublicKey struct {
	N *big.Int
	E *big.Int
}

// ECCPublicKey is an ECC public key extracted from tag 7F49.
// The actual curve constants are provided by the sibling package internal/c2c/bp;
// this package only records the Curve identifier and the (X, Y) coordinates.
type ECCPublicKey struct {
	Curve KeyAlg // one of AlgBrainpool*
	X, Y  *big.Int
}

// Cert is a decoded CV-certificate. Body and Raw alias into the original
// input slice — do not mutate. Body contains the bytes a verifier needs to
// hash (the full 7F4E TLV including the 7F4E tag and length).
type Cert struct {
	CPI       byte
	CAR       string
	CHR       string
	NotBefore time.Time
	NotAfter  time.Time
	KeyAlg    KeyAlg
	PublicKey any    // *RSAPublicKey or *ECCPublicKey
	CHAT      []byte // raw 7F4C value (nil if absent)
	Signature []byte // raw 5F37 value
	Body      []byte // raw 7F4E TLV — the signed-over bytes
	Raw       []byte // entire 7F21 wrapper as received (nil if input was bare 7F4E)
}

// Parse decodes a CV-certificate. The input may be either the outer 7F21
// wrapper or a bare 7F4E body (in which case there is no signature; an
// error is returned if the body is malformed but a body-only input with no
// signature succeeds with Signature == nil and Raw == nil).
func Parse(der []byte) (*Cert, error) {
	if len(der) == 0 {
		return nil, errors.New("cvcert: empty input")
	}
	// Peek the first tag.
	tag, _, _, err := peekTag(der)
	if err != nil {
		return nil, fmt.Errorf("cvcert: %w", err)
	}
	switch tag {
	case tagCVCert:
		return parseWrapper(der)
	case tagCertBody:
		return ParseBody(der)
	default:
		return nil, fmt.Errorf("cvcert: unexpected outer tag 0x%X (want 0x7F21 or 0x7F4E)", tag)
	}
}

// ParseBody decodes only the 7F4E certificate body. Used when the body and
// signature live in separate APDU responses.
func ParseBody(der []byte) (*Cert, error) {
	if len(der) == 0 {
		return nil, errors.New("cvcert: empty input")
	}
	tag, _, _, err := peekTag(der)
	if err != nil {
		return nil, fmt.Errorf("cvcert: %w", err)
	}
	if tag != tagCertBody {
		return nil, fmt.Errorf("cvcert: ParseBody: expected 0x7F4E, got 0x%X", tag)
	}
	c := &Cert{}
	if err := decodeBody(der, c); err != nil {
		return nil, err
	}
	return c, nil
}

// parseWrapper handles a 7F21 input that contains a 7F4E body followed by
// a 5F37 signature.
func parseWrapper(der []byte) (*Cert, error) {
	t, contents, consumed, err := readTLV(der)
	if err != nil {
		return nil, fmt.Errorf("cvcert: outer: %w", err)
	}
	if t != tagCVCert {
		return nil, fmt.Errorf("cvcert: outer tag 0x%X, want 0x7F21", t)
	}
	if consumed != len(der) {
		return nil, fmt.Errorf("cvcert: %d trailing bytes after 7F21", len(der)-consumed)
	}

	c := &Cert{Raw: der[:consumed]}

	// Body is the first inner TLV, fully retained as raw bytes.
	bodyTag, _, bodyConsumed, err := peekTag(contents)
	if err != nil {
		return nil, fmt.Errorf("cvcert: body: %w", err)
	}
	if bodyTag != tagCertBody {
		return nil, fmt.Errorf("cvcert: expected 7F4E body inside 7F21, got 0x%X", bodyTag)
	}
	if bodyConsumed > len(contents) {
		return nil, errors.New("cvcert: body length overflows wrapper")
	}
	bodyTLV := contents[:bodyConsumed]
	if err := decodeBody(bodyTLV, c); err != nil {
		return nil, err
	}

	// Remainder must be a 5F37 signature.
	rest := contents[bodyConsumed:]
	if len(rest) == 0 {
		return nil, errors.New("cvcert: missing 5F37 signature inside 7F21")
	}
	sigTag, sigVal, sigConsumed, err := readTLV(rest)
	if err != nil {
		return nil, fmt.Errorf("cvcert: signature: %w", err)
	}
	if sigTag != tagSignature {
		return nil, fmt.Errorf("cvcert: expected 5F37 signature, got 0x%X", sigTag)
	}
	if sigConsumed != len(rest) {
		return nil, fmt.Errorf("cvcert: %d trailing bytes after 5F37", len(rest)-sigConsumed)
	}
	c.Signature = sigVal
	return c, nil
}

// decodeBody parses a 7F4E TLV (passed in full, including header) and fills
// the relevant fields in c. c.Body is set to the input slice.
func decodeBody(bodyTLV []byte, c *Cert) error {
	t, contents, consumed, err := readTLV(bodyTLV)
	if err != nil {
		return fmt.Errorf("cvcert: body TLV: %w", err)
	}
	if t != tagCertBody {
		return fmt.Errorf("cvcert: body tag 0x%X, want 0x7F4E", t)
	}
	if consumed != len(bodyTLV) {
		return fmt.Errorf("cvcert: %d trailing bytes after 7F4E body", len(bodyTLV)-consumed)
	}
	c.Body = bodyTLV[:consumed]

	// Iterate the body's inner TLVs. The order in BSI TR-03110 §C.1 is
	// fixed but we accept any order to be robust against minor profile
	// variations — what matters is that every required field appears
	// exactly once.
	seen := map[uint32]bool{}
	rest := contents
	for len(rest) > 0 {
		innerTag, innerVal, innerN, err := readTLV(rest)
		if err != nil {
			return fmt.Errorf("cvcert: body inner TLV: %w", err)
		}
		if seen[innerTag] {
			return fmt.Errorf("cvcert: duplicate body tag 0x%X", innerTag)
		}
		seen[innerTag] = true

		switch innerTag {
		case tagCPI:
			if len(innerVal) != 1 {
				return fmt.Errorf("cvcert: CPI length %d, want 1", len(innerVal))
			}
			c.CPI = innerVal[0]
		case tagCAR:
			c.CAR = string(innerVal)
		case tagPubKey:
			if err := decodePublicKey(innerVal, c); err != nil {
				return err
			}
		case tagCHR:
			c.CHR = string(innerVal)
		case tagCHAT:
			// Copy so callers can hold onto it independently of input.
			c.CHAT = append([]byte(nil), innerVal...)
		case tagEffective:
			t, err := decodeBCDDate(innerVal)
			if err != nil {
				return fmt.Errorf("cvcert: effective date: %w", err)
			}
			c.NotBefore = t
		case tagExpiration:
			t, err := decodeBCDDate(innerVal)
			if err != nil {
				return fmt.Errorf("cvcert: expiration date: %w", err)
			}
			c.NotAfter = t
		default:
			// Unknown tags inside the body are tolerated but ignored — the
			// signed bytes still include them. TODO(spec): some profiles
			// add extension tags we may want to surface.
		}
		rest = rest[innerN:]
	}

	// Mandatory fields.
	for _, m := range []struct {
		tag  uint32
		name string
	}{
		{tagCPI, "CPI (5F29)"},
		{tagCAR, "CAR (42)"},
		{tagPubKey, "PublicKey (7F49)"},
		{tagCHR, "CHR (5F20)"},
		{tagEffective, "EffectiveDate (5F25)"},
		{tagExpiration, "ExpirationDate (5F24)"},
	} {
		if !seen[m.tag] {
			return fmt.Errorf("cvcert: missing required field %s", m.name)
		}
	}
	return nil
}

// decodePublicKey parses the contents of a 7F49 TLV (its value, not the TLV
// itself). It picks RSA vs ECC based on the embedded OID.
func decodePublicKey(val []byte, c *Cert) error {
	// First inner TLV must be the algorithm OID.
	tag, oid, n, err := readTLV(val)
	if err != nil {
		return fmt.Errorf("cvcert: pubkey: %w", err)
	}
	if tag != tagOID {
		return fmt.Errorf("cvcert: pubkey first inner tag 0x%X, want 0x06 (OID)", tag)
	}
	rest := val[n:]

	switch {
	case bytes.Equal(oid, oidRSAv15SHA256):
		c.KeyAlg = AlgRSA2048
		pk, err := decodeRSAKey(rest)
		if err != nil {
			return err
		}
		c.PublicKey = pk
	case bytes.Equal(oid, oidECDSAbp256):
		c.KeyAlg = AlgBrainpoolP256r1
		pk, err := decodeECCKey(rest, AlgBrainpoolP256r1, 32)
		if err != nil {
			return err
		}
		c.PublicKey = pk
	case bytes.Equal(oid, oidECDSAbp384):
		c.KeyAlg = AlgBrainpoolP384r1
		pk, err := decodeECCKey(rest, AlgBrainpoolP384r1, 48)
		if err != nil {
			return err
		}
		c.PublicKey = pk
	case bytes.Equal(oid, oidECDSAbp512):
		c.KeyAlg = AlgBrainpoolP512r1
		pk, err := decodeECCKey(rest, AlgBrainpoolP512r1, 64)
		if err != nil {
			return err
		}
		c.PublicKey = pk
	default:
		return fmt.Errorf("cvcert: unknown public-key OID %X", oid)
	}
	return nil
}

func decodeRSAKey(rest []byte) (*RSAPublicKey, error) {
	var n, e *big.Int
	for len(rest) > 0 {
		tag, val, consumed, err := readTLV(rest)
		if err != nil {
			return nil, fmt.Errorf("cvcert: RSA key: %w", err)
		}
		switch tag {
		case tagRSAModulus:
			if n != nil {
				return nil, errors.New("cvcert: duplicate RSA modulus (81)")
			}
			n = new(big.Int).SetBytes(val)
		case tagRSAExp:
			if e != nil {
				return nil, errors.New("cvcert: duplicate RSA exponent (82)")
			}
			e = new(big.Int).SetBytes(val)
		default:
			return nil, fmt.Errorf("cvcert: unexpected tag 0x%X in RSA key", tag)
		}
		rest = rest[consumed:]
	}
	if n == nil {
		return nil, errors.New("cvcert: RSA key missing modulus (81)")
	}
	if e == nil {
		return nil, errors.New("cvcert: RSA key missing exponent (82)")
	}
	return &RSAPublicKey{N: n, E: e}, nil
}

func decodeECCKey(rest []byte, alg KeyAlg, coordLen int) (*ECCPublicKey, error) {
	var pt []byte
	for len(rest) > 0 {
		tag, val, consumed, err := readTLV(rest)
		if err != nil {
			return nil, fmt.Errorf("cvcert: ECC key: %w", err)
		}
		switch tag {
		case tagECPoint:
			if pt != nil {
				return nil, errors.New("cvcert: duplicate ECC point (86)")
			}
			pt = val
		default:
			// Some profiles include domain-parameter tags (81..85). We
			// tolerate them but do not validate them — Stream B owns curve
			// math. TODO(spec): cross-check accepted tag list.
		}
		rest = rest[consumed:]
	}
	if pt == nil {
		return nil, errors.New("cvcert: ECC key missing public point (86)")
	}
	if len(pt) != 1+2*coordLen {
		return nil, fmt.Errorf("cvcert: ECC point length %d, want %d for %s",
			len(pt), 1+2*coordLen, alg)
	}
	if pt[0] != 0x04 {
		return nil, fmt.Errorf("cvcert: ECC point not uncompressed (first byte 0x%02X, want 0x04)", pt[0])
	}
	x := new(big.Int).SetBytes(pt[1 : 1+coordLen])
	y := new(big.Int).SetBytes(pt[1+coordLen:])
	return &ECCPublicKey{Curve: alg, X: x, Y: y}, nil
}

// decodeBCDDate parses a 6-byte packed-BCD date YYMMDD. Each nibble must be
// 0..9. The year is 2000 + YY. The day is validated against the calendar.
//
// Spec note: gemSpec_PKI uses packed BCD; some profiles use ASCII digits.
// We accept either: if every byte is a printable ASCII digit, we treat the
// field as ASCII. Otherwise we treat it as packed BCD.
// TODO(spec): confirm which encoding gemSpec_PKI mandates for C2C certs
// and tighten this if needed.
func decodeBCDDate(val []byte) (time.Time, error) {
	if len(val) != 6 {
		return time.Time{}, fmt.Errorf("date length %d, want 6", len(val))
	}
	digits := make([]int, 6)
	// Try ASCII first.
	asciiOK := true
	for i, b := range val {
		if b < '0' || b > '9' {
			asciiOK = false
			break
		}
		digits[i] = int(b - '0')
	}
	if !asciiOK {
		// Packed BCD: each byte 0x0N where N is a digit 0..9.
		for i, b := range val {
			if b > 9 {
				return time.Time{}, fmt.Errorf("byte %d (0x%02X) is not a BCD digit", i, b)
			}
			digits[i] = int(b)
		}
	}
	year := 2000 + digits[0]*10 + digits[1]
	month := digits[2]*10 + digits[3]
	day := digits[4]*10 + digits[5]
	if month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("invalid month %02d", month)
	}
	if day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("invalid day %02d", day)
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	// time.Date normalises overflow; reject if the round-trip changed it.
	if t.Year() != year || t.Month() != time.Month(month) || t.Day() != day {
		return time.Time{}, fmt.Errorf("invalid date %04d-%02d-%02d", year, month, day)
	}
	return t, nil
}

// readTLV decodes one BER-TLV record from the front of buf and returns the
// tag, value slice (aliasing into buf), and number of bytes consumed.
func readTLV(buf []byte) (tag uint32, value []byte, consumed int, err error) {
	tag, tagLen, err := readTag(buf)
	if err != nil {
		return 0, nil, 0, err
	}
	length, lenLen, err := readLength(buf[tagLen:])
	if err != nil {
		return 0, nil, 0, err
	}
	headerLen := tagLen + lenLen
	if length > len(buf)-headerLen {
		return 0, nil, 0, fmt.Errorf("TLV value (%d bytes) overruns buffer (%d available)",
			length, len(buf)-headerLen)
	}
	return tag, buf[headerLen : headerLen+length], headerLen + length, nil
}

// peekTag is like readTLV but reads only the tag and computes the total
// record length without copying the value. It is used when we want to keep
// the original TLV bytes (header + value) intact.
func peekTag(buf []byte) (tag uint32, tagLen int, totalLen int, err error) {
	tag, tagLen, err = readTag(buf)
	if err != nil {
		return 0, 0, 0, err
	}
	length, lenLen, err := readLength(buf[tagLen:])
	if err != nil {
		return 0, 0, 0, err
	}
	total := tagLen + lenLen + length
	if total > len(buf) {
		return 0, 0, 0, fmt.Errorf("TLV (tag 0x%X) overruns buffer", tag)
	}
	return tag, tagLen, total, nil
}

// readTag decodes a BER tag from the front of buf. Returns the tag as a
// big-endian uint32 (so 7F21 stays 0x7F21) and the number of bytes consumed.
// Tags up to 4 bytes are supported; CV-certs use at most 2-byte tags.
func readTag(buf []byte) (uint32, int, error) {
	if len(buf) == 0 {
		return 0, 0, errors.New("tag: empty buffer")
	}
	first := buf[0]
	tag := uint32(first)
	// Multi-byte tag: low 5 bits all set.
	if first&0x1F == 0x1F {
		n := 1
		for {
			if n >= len(buf) {
				return 0, 0, errors.New("tag: truncated multi-byte tag")
			}
			if n > 4 {
				return 0, 0, errors.New("tag: tag longer than 4 bytes")
			}
			b := buf[n]
			tag = (tag << 8) | uint32(b)
			n++
			if b&0x80 == 0 {
				return tag, n, nil
			}
		}
	}
	return tag, 1, nil
}

// readLength decodes a BER length from the front of buf. Returns the value
// length and the number of bytes consumed by the length encoding.
func readLength(buf []byte) (int, int, error) {
	if len(buf) == 0 {
		return 0, 0, errors.New("length: empty buffer")
	}
	first := buf[0]
	if first < 0x80 {
		return int(first), 1, nil
	}
	if first == 0x80 {
		return 0, 0, errors.New("length: indefinite form not allowed in DER")
	}
	n := int(first & 0x7F)
	if n > 4 {
		return 0, 0, fmt.Errorf("length: %d-byte length not supported", n)
	}
	if 1+n > len(buf) {
		return 0, 0, errors.New("length: truncated long form")
	}
	v := 0
	for i := 0; i < n; i++ {
		v = (v << 8) | int(buf[1+i])
	}
	// Disallow non-minimal encodings only weakly — gemSpec sometimes uses
	// 81 LL for lengths < 128. We accept both.
	return v, 1 + n, nil
}
