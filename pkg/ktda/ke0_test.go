package ktda

import (
	"bytes"
	"strings"
	"testing"
)

// buildKE0 assembles a minimal KE0-style byte stream using ASCII (the Latin-1
// decoder passes ASCII through unchanged).
func buildKE0(segments ...string) []byte {
	return []byte(strings.Join(segments, "'") + "'")
}

func TestParseMinimalEntry(t *testing.T) {
	src := buildKE0(
		"UNH+1+KOTRDA:14:0:0",
		"IDK+109519005+02+TK+12345",
		"VDT+20240101+20251231",
		"NAM+1+Techniker+Krankenkasse",
		"VKG+01+109519005+5+109519005",
		"UNT+5+1",
	)
	entries, err := Parse(bytes.NewReader(src), "EK")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.IK != "109519005" {
		t.Errorf("IK = %q", e.IK)
	}
	if e.ShortName != "TK" {
		t.Errorf("ShortName = %q", e.ShortName)
	}
	if e.VKNR != "12345" {
		t.Errorf("VKNR = %q", e.VKNR)
	}
	if e.Kassenart != "EK" {
		t.Errorf("Kassenart = %q", e.Kassenart)
	}
	if e.Name != "Techniker Krankenkasse" {
		t.Errorf("Name = %q", e.Name)
	}
	if e.ValidFrom != "20240101" || e.ValidTo != "20251231" {
		t.Errorf("Validity = %q / %q", e.ValidFrom, e.ValidTo)
	}
	if len(e.Links) != 1 {
		t.Fatalf("Links = %d", len(e.Links))
	}
	if e.Links[0].Art != "01" || e.Links[0].IK != "109519005" {
		t.Errorf("Link = %+v", e.Links[0])
	}
}

func TestParseMultipleMessages(t *testing.T) {
	src := buildKE0(
		"UNH+1",
		"IDK+1++Alpha",
		"UNT+2+1",
		"UNH+2",
		"IDK+2++Beta",
		"UNT+2+2",
	)
	entries, err := Parse(bytes.NewReader(src), "AO")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].IK != "1" || entries[1].IK != "2" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestParseFinalFlushWithoutUNT(t *testing.T) {
	// File without trailing UNT — final entry still flushed at EOF.
	src := buildKE0(
		"UNH+1",
		"IDK+1++Alpha",
	)
	entries, err := Parse(bytes.NewReader(src), "BK")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 || entries[0].IK != "1" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestParseUNHFlushesPending(t *testing.T) {
	// Two UNHs in a row (no UNT between) — the first pending entry should
	// still be flushed on the second UNH.
	src := buildKE0(
		"UNH+1",
		"IDK+1++Alpha",
		"UNH+2",
		"IDK+2++Beta",
		"UNT+2+2",
	)
	entries, err := Parse(bytes.NewReader(src), "IK")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d (%+v)", len(entries), entries)
	}
}

func TestParseSegmentsBeforeUNHCreateEntry(t *testing.T) {
	// IDK without a preceding UNH — handleSegment allocates a new Entry.
	src := buildKE0(
		"IDK+999++OrphanShort",
		"UNT+2+1", // closes the entry
	)
	entries, err := Parse(bytes.NewReader(src), "BN")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].IK != "999" || entries[0].ShortName != "OrphanShort" {
		t.Errorf("entry = %+v", entries[0])
	}
}

func TestParseNAMVDTVKGWithoutCurrentEntryNoOps(t *testing.T) {
	// VDT/NAM/VKG before any UNH/IDK — handleSegment returns nil and discards.
	src := buildKE0(
		"VDT+20240101+20251231",
		"NAM+1+Should+Be+Dropped",
		"VKG+01+5+5+5",
		"UNH+1",
		"IDK+1++X",
		"UNT+2+1",
	)
	entries, err := Parse(bytes.NewReader(src), "LK")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].ValidFrom != "" || entries[0].Name != "" || len(entries[0].Links) != 0 {
		t.Errorf("stray segments leaked: %+v", entries[0])
	}
}

func TestParseEmbeddedCRLF(t *testing.T) {
	// Segments split across line breaks — parser ignores CR/LF.
	src := []byte("UNH+1'\r\nIDK+1++X'\r\nUNT+2+1'\r\n")
	entries, err := Parse(bytes.NewReader(src), "AO")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d", len(entries))
	}
}

func TestParseEDIFACTEscape(t *testing.T) {
	// "?+" is the EDIFACT escape for a literal "+" inside a field.
	src := buildKE0(
		"UNH+1",
		"IDK+1++Name?+with?+pluses",
		"UNT+2+1",
	)
	entries, err := Parse(bytes.NewReader(src), "AO")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if entries[0].ShortName != "Name+with+pluses" {
		t.Errorf("ShortName = %q", entries[0].ShortName)
	}
}

func TestSplitEDIFACT(t *testing.T) {
	got := splitEDIFACT("A+B+C")
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Errorf("plain: %v", got)
	}
	// Escape character.
	got = splitEDIFACT("A?+B+C")
	if len(got) != 2 || got[0] != "A+B" || got[1] != "C" {
		t.Errorf("escape: %v", got)
	}
	// Trailing empty field.
	got = splitEDIFACT("A+")
	if len(got) != 2 || got[0] != "A" || got[1] != "" {
		t.Errorf("trailing: %v", got)
	}
}

func TestParseShortSegmentIgnored(t *testing.T) {
	// 2-character segment — too short for a tag, ignored without error.
	src := []byte("AB'UNH+1'IDK+1++X'UNT+2+1'")
	entries, err := Parse(bytes.NewReader(src), "AO")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d", len(entries))
	}
}

func TestParseEmpty(t *testing.T) {
	entries, err := Parse(bytes.NewReader(nil), "AO")
	if err != nil {
		t.Fatalf("Parse(empty): %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestNAMSkipsEmptyParts(t *testing.T) {
	// Empty Name3 between Name2 and Name4 — should be dropped from the join.
	src := buildKE0(
		"UNH+1",
		"IDK+1",
		"NAM+1+First++Third",
		"UNT+2+1",
	)
	entries, err := Parse(bytes.NewReader(src), "AO")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if entries[0].Name != "First Third" {
		t.Errorf("Name = %q", entries[0].Name)
	}
}

func TestNAMOnlyFirstSeen(t *testing.T) {
	// Two NAM segments for the same entry — only the first populates Name.
	src := buildKE0(
		"UNH+1",
		"IDK+1",
		"NAM+1+Alpha",
		"NAM+2+Beta",
		"UNT+3+1",
	)
	entries, err := Parse(bytes.NewReader(src), "AO")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if entries[0].Name != "Alpha" {
		t.Errorf("Name = %q", entries[0].Name)
	}
}

func TestVDTOnlyFirstValidFromKept(t *testing.T) {
	src := buildKE0(
		"UNH+1",
		"IDK+1",
		"VDT+20240101+20250101",
		"VDT+20230101+20260101",
		"UNT+3+1",
	)
	entries, err := Parse(bytes.NewReader(src), "AO")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// ValidFrom kept from first VDT; ValidTo overwritten by second.
	if entries[0].ValidFrom != "20240101" {
		t.Errorf("ValidFrom = %q", entries[0].ValidFrom)
	}
	if entries[0].ValidTo != "20260101" {
		t.Errorf("ValidTo = %q", entries[0].ValidTo)
	}
}

func TestParseErrorString(t *testing.T) {
	pe := &ParseError{Reason: "bogus"}
	if pe.Error() != "not a KE0 file: bogus" {
		t.Errorf("Error = %q", pe.Error())
	}
}
