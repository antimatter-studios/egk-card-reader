package egk

import (
	"testing"
)

func TestParseICCSN_TLV(t *testing.T) {
	// 5A 0A <10 bytes> — canonical form.
	tlv := []byte{0x5A, 0x0A, 0x80, 0x27, 0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	got := parseICCSN(tlv)
	if got != "80276000000000000000" {
		t.Errorf("ICCSN = %q", got)
	}
}

func TestParseICCSN_Raw10(t *testing.T) {
	raw := []byte{0x80, 0x27, 0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	got := parseICCSN(raw)
	if got != "80276000000000000000" {
		t.Errorf("ICCSN = %q", got)
	}
}

func TestParseICCSN_RejectsMalformed(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		{0x01, 0x02},                  // too short
		{0x5A, 0x0A, 0x00},            // tag-len OK but value truncated
		{0x5B, 0x0A, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // wrong tag
		{0x5A, 0x05, 0, 0, 0, 0, 0},   // wrong length (not 10)
	}
	for i, c := range cases {
		if got := parseICCSN(c); got != "" {
			t.Errorf("case %d: parseICCSN(%v) = %q, want empty", i, c, got)
		}
	}
}

func TestReadMF_Happy(t *testing.T) {
	// SELECT EF.GDO (2F02) then READ BINARY 32 bytes.
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C022F02", sw: 0x9000},
			{wantAPDU: "00B0000020",
				respData: []byte{0x5A, 0x0A, 0x80, 0x27, 0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
				sw:       0x9000},
		},
	}
	mf, err := readMF(card)
	if err != nil {
		t.Fatal(err)
	}
	if mf.ICCSN != "80276000000000000000" {
		t.Errorf("ICCSN = %q", mf.ICCSN)
	}
	if len(mf.GDO) != 12 {
		t.Errorf("GDO len = %d, want 12", len(mf.GDO))
	}
}

func TestReadMF_SelectFails(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C022F02", sw: 0x6A82},
		},
	}
	if _, err := readMF(card); err == nil {
		t.Error("expected error when SELECT EF.GDO fails")
	}
}

func TestReadMF_ReadFails(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C022F02", sw: 0x9000},
			{wantAPDU: "00B0000020", sw: 0x6982}, // not allowed
		},
	}
	if _, err := readMF(card); err == nil {
		t.Error("expected error when READ BINARY fails")
	}
}

func TestHexDotted(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte{}, ""},
		{[]byte{0x04, 0x05, 0x02}, "04.05.02"},
		{[]byte{0x80, 0x27, 0x60}, "80.27.60"},
		{[]byte{0xFF}, "FF"},
	}
	for _, c := range cases {
		if got := hexDotted(c.in); got != c.want {
			t.Errorf("hexDotted(%X) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAsciiPart(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte("DEDIMEHC_9000\x03\x00\x05"), "DEDIMEHC_9000"},
		{[]byte{0x00, 0x01}, ""},
		{[]byte("hello"), "hello"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := asciiPart(c.in); got != c.want {
			t.Errorf("asciiPart(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseVersion2_FourTags(t *testing.T) {
	// 0xEF <len> C0 03 04 05 02 C1 03 02 00 00 C2 04 41 42 43 03 C3 03 01 02 03
	raw := []byte{
		0xEF, 0x15,
		0xC0, 0x03, 0x04, 0x05, 0x02,
		0xC1, 0x03, 0x02, 0x00, 0x00,
		0xC2, 0x04, 0x41, 0x42, 0x43, 0x03, // "ABC" + binary
		0xC3, 0x03, 0x01, 0x02, 0x03,
	}
	v := parseVersion2(raw)
	if v.TagC0 != "04.05.02" {
		t.Errorf("C0 = %q", v.TagC0)
	}
	if v.TagC1 != "02.00.00" {
		t.Errorf("C1 = %q", v.TagC1)
	}
	// C2 only has "ABC" (3 chars), below the >=4 ASCII threshold → hex only.
	if v.TagC2 != "41.42.43.03" {
		t.Errorf("C2 = %q", v.TagC2)
	}
	if v.TagC3 != "01.02.03" {
		t.Errorf("C3 = %q", v.TagC3)
	}
}

func TestParseVersion2_AsciiPrefix(t *testing.T) {
	// C2 with 13 printable ASCII bytes then binary → ASCII prefix surfaced.
	raw := []byte{
		0xEF, 0x12,
		0xC2, 0x10,
		'D', 'E', 'D', 'I', 'M', 'E', 'H', 'C', '_', '9', '0', '0', '0', 0x03, 0x00, 0x05,
	}
	v := parseVersion2(raw)
	if v.TagC2 != "DEDIMEHC_9000 (44.45.44.49.4D.45.48.43.5F.39.30.30.30.03.00.05)" {
		t.Errorf("C2 = %q", v.TagC2)
	}
}

func TestParseVersion2_NoOuterTag(t *testing.T) {
	// Some cards return the children directly without the outer EF wrapper.
	raw := []byte{0xC0, 0x03, 0x04, 0x05, 0x02}
	v := parseVersion2(raw)
	if v.TagC0 != "04.05.02" {
		t.Errorf("C0 = %q", v.TagC0)
	}
}

func TestReadVersion2_HappyD080(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C02D080", sw: 0x9000},
			{wantAPDU: "00B0000040",
				respData: []byte{0xEF, 0x05, 0xC0, 0x03, 0x04, 0x05, 0x02},
				sw:       0x9000},
		},
	}
	v, err := readVersion2(card)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil || v.FID != 0xD080 {
		t.Fatalf("FID = %v", v)
	}
	if v.TagC0 != "04.05.02" {
		t.Errorf("C0 = %q", v.TagC0)
	}
}

func TestReadVersion2_FallbackTo2F11(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C02D080", sw: 0x6A82}, // not present
			{wantAPDU: "00A4020C022F11", sw: 0x9000}, // legacy hit
			{wantAPDU: "00B0000040",
				respData: []byte{0xC0, 0x03, 0x01, 0x02, 0x03},
				sw:       0x9000},
		},
	}
	v, err := readVersion2(card)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil || v.FID != 0x2F11 {
		t.Fatalf("FID = %v", v)
	}
}

func TestReadVersion2_NoneFound(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C02D080", sw: 0x6A82},
			{wantAPDU: "00A4020C022F11", sw: 0x6A82},
		},
	}
	v, err := readVersion2(card)
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Errorf("expected nil Version, got %+v", v)
	}
}
