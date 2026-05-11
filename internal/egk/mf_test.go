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
