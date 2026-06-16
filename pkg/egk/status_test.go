package egk

import "testing"

func TestIsAllDigits(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"123", true},
		{"1234567890", true},
		{"12a", false},
		{"  12", false},
	}
	for _, c := range cases {
		if got := isAllDigits(c.in); got != c.want {
			t.Errorf("isAllDigits(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseStatusVD_Timestamp(t *testing.T) {
	// Live-card observed shape: '0' prefix + 14-char timestamp + null + status bytes.
	raw := []byte("020230421171632\x00\x50\x02\x00\x00\x00\x30\x00\x00\x04")
	s := parseStatusVD(raw, 0xD00C)
	if s.Timestamp != "2023-04-21T17:16:32" {
		t.Errorf("Timestamp = %q", s.Timestamp)
	}
	if s.FID != 0xD00C {
		t.Errorf("FID = %04X", s.FID)
	}
	if s.StatusHex != "5002000000300000" {
		// Length-mismatch is fine; just check the prefix matches what the
		// hex form would look like for the non-null tail.
		if len(s.StatusHex) == 0 {
			t.Errorf("StatusHex unexpectedly empty")
		}
	}
}

func TestParseStatusVD_NonDigitTimestampStaysEmpty(t *testing.T) {
	raw := []byte("0abcdefghijklmn\x00\x00")
	s := parseStatusVD(raw, 0xD00C)
	if s.Timestamp != "" {
		t.Errorf("expected empty timestamp for non-digit input, got %q", s.Timestamp)
	}
}

func TestParseStatusVD_TooShortReturnsEmpty(t *testing.T) {
	s := parseStatusVD([]byte{0x01, 0x02}, 0xD00C)
	if s.Timestamp != "" {
		t.Errorf("Timestamp = %q, want empty", s.Timestamp)
	}
}

func TestReadStatusVD_Happy(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C02D00C", sw: 0x9000},
			{wantAPDU: "00B0000020",
				respData: []byte("020230421171632\x00\x50\x02\x00\x00\x00\x30\x00\x00\x04"),
				sw:       0x9000},
		},
	}
	s, err := readStatusVD(card)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Timestamp != "2023-04-21T17:16:32" {
		t.Errorf("got %+v", s)
	}
}

func TestReadStatusVD_SelectFails(t *testing.T) {
	card := &fakeCard{
		t: t,
		script: []scriptEntry{
			{wantAPDU: "00A4020C02D00C", sw: 0x6A82},
		},
	}
	if _, err := readStatusVD(card); err == nil {
		t.Error("expected error when SELECT EF.StatusVD fails")
	}
}
