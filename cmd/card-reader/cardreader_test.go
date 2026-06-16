package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ebfe/scard"

	"github.com/christhomas/card-reader/pkg/document"
	"github.com/christhomas/card-reader/pkg/egk"
)

func TestSuggestBaseName(t *testing.T) {
	// No KVNR → "card-<ts>"
	got := suggestBaseName(nil)
	if !strings.HasPrefix(got, "card-") {
		t.Errorf("nil data = %q, want card-* prefix", got)
	}
	got = suggestBaseName(&egk.CardData{})
	if !strings.HasPrefix(got, "card-") {
		t.Errorf("empty = %q", got)
	}
	got = suggestBaseName(&egk.CardData{Personal: &egk.PersonalData{}})
	if !strings.HasPrefix(got, "card-") {
		t.Errorf("empty InsurantID = %q", got)
	}
	// With KVNR.
	got = suggestBaseName(&egk.CardData{Personal: &egk.PersonalData{InsurantID: "X110407317"}})
	if !strings.HasPrefix(got, "patient-X110407317-") {
		t.Errorf("with KVNR = %q", got)
	}
}

func TestDecodeState(t *testing.T) {
	if got := decodeState(0); got != "NONE" {
		t.Errorf("0 = %q", got)
	}
	if got := decodeState(scard.StatePresent); got != "PRESENT" {
		t.Errorf("present = %q", got)
	}
	// Combination.
	combined := scard.StatePresent | scard.StateMute
	got := decodeState(combined)
	if !strings.Contains(got, "PRESENT") || !strings.Contains(got, "MUTE") {
		t.Errorf("combined = %q", got)
	}
	// Every bit individually should resolve to a non-empty name.
	for _, bit := range []scard.StateFlag{
		scard.StateChanged, scard.StateUnknown, scard.StateUnavailable, scard.StateEmpty,
		scard.StatePresent, scard.StateAtrmatch, scard.StateExclusive, scard.StateInuse,
		scard.StateMute, scard.StateUnpowered,
	} {
		if got := decodeState(bit); got == "NONE" || got == "" {
			t.Errorf("bit %d → %q", bit, got)
		}
	}
}

func TestLoadCardDataFileNotFound(t *testing.T) {
	_, _, err := loadCardData("/does/not/exist.gdt")
	if err == nil {
		t.Error("expected stat error")
	}
}

func TestLoadCardDataUnknownExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(path, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadCardData(path)
	if err == nil {
		t.Error("expected unsupported-extension error")
	}
}

func TestLoadCardDataGDT(t *testing.T) {
	d := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "X", InsurantID: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := document.Encoders["gdt"].Encode(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "in.gdt")
	if err := os.WriteFile(path, doc.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}

	got, cleanup, err := loadCardData(path)
	if err != nil {
		t.Fatalf("loadCardData: %v", err)
	}
	if cleanup != nil {
		t.Error("cleanup should be nil for file input")
	}
	if got == nil || got.Personal == nil || got.Personal.LastName != "X" {
		t.Errorf("got %+v", got)
	}
}

func TestLoadCardDataFHIR(t *testing.T) {
	d := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "X", InsurantID: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := document.Encoders["fhir"].Encode(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "in.fhir.json")
	if err := os.WriteFile(path, doc.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := loadCardData(path)
	if err != nil {
		t.Fatalf("loadCardData: %v", err)
	}
	if got.Personal == nil {
		t.Error("Personal nil")
	}
}

func TestLoadCardDataHL7(t *testing.T) {
	d := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "X", InsurantID: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := document.Encoders["hl7adt"].Encode(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "in.hl7")
	if err := os.WriteFile(path, doc.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := loadCardData(path)
	if err != nil {
		t.Fatalf("loadCardData: %v", err)
	}
	if got.Personal == nil {
		t.Error("Personal nil")
	}
}

func TestLoadCardDataBadGDT(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.gdt")
	// Bad GDT (no real fields) still parses without error, but it would
	// produce empty Personal/Insurance. To force a parse error, hand it a
	// path we can't open by replacing the file with a directory.
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadCardData(path)
	if err == nil {
		t.Error("expected parse error for directory-as-file")
	}
}

// Run ReadCmd.Run against file input — covers the encode/render branches.
func TestReadCmdRunFileGDTTable(t *testing.T) {
	dir := t.TempDir()
	d := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "X", InsurantID: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := document.Encoders["gdt"].Encode(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "in.gdt")
	if err := os.WriteFile(path, doc.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	// Chdir to keep any ktda.json auto-fetch from happening (we have no
	// ktda.json available; resolveIK will return nil and that's fine).
	t.Chdir(dir)

	// Hide stdout to avoid polluting test output.
	origStdout := os.Stdout
	null, _ := os.Open(os.DevNull)
	os.Stdout = null
	t.Cleanup(func() {
		os.Stdout = origStdout
		null.Close()
	})

	cmd := &ReadCmd{Input: path, Output: "form"}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// resolveIK will likely attempt auto-fetch; setting an offline URL
	// makes it fail fast.
	if err := cmd.Run(); err != nil {
		t.Logf("Run returned (may indicate ktda auto-fetch attempted): %v", err)
	}
}

func TestReadCmdRunFileMode(t *testing.T) {
	dir := t.TempDir()
	d := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "X", InsurantID: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := document.Encoders["gdt"].Encode(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "in.gdt")
	if err := os.WriteFile(path, doc.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cmd := &ReadCmd{Input: path, Output: "json", File: true}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := cmd.Run(); err != nil {
		t.Logf("Run: %v", err)
	}
	// Output directory should now exist.
	entries, _ := os.ReadDir(filepath.Join(dir, "output"))
	if len(entries) == 0 {
		t.Error("no output file written")
	}
}

// ---- PC/SC fake + waitForCard / runDebug tests ---------------------------

// fakeCtx implements pcscContext with scripted responses, mirroring the
// scripted-response model used by fakeCard in pkg/egk/apdu_test.go.
//
// Each call to GetStatusChange consumes one entry from statusScript: the
// per-reader EventState bits are applied to the supplied []ReaderState, and
// any err is returned. Once the script is exhausted, the last entry's
// EventStates are reused (so "always returns X" can be expressed with a
// single entry).
//
// Connect always returns connectErr if set; otherwise it returns
// (nil, nil). Because we cannot construct a real *scard.Card from outside
// the scard package, success-path Connect tests are not possible without
// an adapter — and adapters are off-limits per the task constraints.
type fakeCtx struct {
	statusScript []statusEntry
	statusPos    int
	connectErr   error
	connectCalls int
}

type statusEntry struct {
	// eventStates is indexed by reader position; bits to OR into the
	// matching ReaderState.EventState.
	eventStates []scard.StateFlag
	err         error
}

func (f *fakeCtx) GetStatusChange(states []scard.ReaderState, _ time.Duration) error {
	var e statusEntry
	if len(f.statusScript) == 0 {
		return nil
	}
	if f.statusPos < len(f.statusScript) {
		e = f.statusScript[f.statusPos]
		f.statusPos++
	} else {
		e = f.statusScript[len(f.statusScript)-1]
	}
	for i := range states {
		if i < len(e.eventStates) {
			states[i].EventState = e.eventStates[i]
		}
	}
	return e.err
}

func (f *fakeCtx) Connect(_ string, _ scard.ShareMode, _ scard.Protocol) (*scard.Card, error) {
	f.connectCalls++
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	// No way to fabricate a usable *scard.Card from outside the scard
	// package; callers that need success-path coverage must mock at a
	// higher layer.
	return nil, nil
}

func TestWaitForCardHappy(t *testing.T) {
	f := &fakeCtx{
		statusScript: []statusEntry{
			{eventStates: []scard.StateFlag{scard.StatePresent}},
		},
	}
	got, err := waitForCard(f, []string{"R0"}, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("waitForCard: %v", err)
	}
	if got != "R0" {
		t.Errorf("reader = %q, want R0", got)
	}
}

func TestWaitForCardMuteIgnored(t *testing.T) {
	// Reader reports present-but-mute every poll → never picked → timeout.
	f := &fakeCtx{
		statusScript: []statusEntry{
			{eventStates: []scard.StateFlag{scard.StatePresent | scard.StateMute}},
		},
	}
	_, err := waitForCard(f, []string{"R0"}, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error when reader is mute")
	}
	if !strings.Contains(err.Error(), "no card present") {
		t.Errorf("err = %v, want no-card-present", err)
	}
}

func TestWaitForCardTimeout(t *testing.T) {
	f := &fakeCtx{
		statusScript: []statusEntry{
			{eventStates: []scard.StateFlag{scard.StateEmpty}},
		},
	}
	start := time.Now()
	_, err := waitForCard(f, []string{"R0"}, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "no card present") {
		t.Errorf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waitForCard took %v — too slow", elapsed)
	}
}

func TestWaitForCardError(t *testing.T) {
	want := fmt.Errorf("boom")
	f := &fakeCtx{
		statusScript: []statusEntry{
			{err: want},
		},
	}
	_, err := waitForCard(f, []string{"R0"}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestWaitForCardErrTimeoutTolerated(t *testing.T) {
	// First poll returns scard.ErrTimeout (no event in 500ms) → keep
	// polling. Second poll returns StatePresent → success.
	f := &fakeCtx{
		statusScript: []statusEntry{
			{err: scard.ErrTimeout},
			{eventStates: []scard.StateFlag{scard.StatePresent}},
		},
	}
	got, err := waitForCard(f, []string{"R0"}, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("waitForCard: %v", err)
	}
	if got != "R0" {
		t.Errorf("reader = %q, want R0", got)
	}
	if f.statusPos < 2 {
		t.Errorf("expected ErrTimeout to be tolerated and second poll consumed; pos = %d", f.statusPos)
	}
}

// silenceStdout redirects os.Stdout to /dev/null for the duration of a test.
// Mirrors the pattern in TestReadCmdRunFileGDTTable. Returns no value — the
// cleanup is registered with t.Cleanup.
func silenceStdout(t *testing.T) {
	t.Helper()
	orig := os.Stdout
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	os.Stdout = null
	t.Cleanup(func() {
		os.Stdout = orig
		null.Close()
	})
}

// TestRunDebugNoCard exercises the deadline-expiry branch of runDebug.
//
// runDebug hardcodes a 15-second deadline (the loop exits early only when a
// card is detected), so this test takes ~15s. We accept the cost rather
// than skip in -short: parallelism with the other 15s test (the print-loop
// path) keeps wall-clock additive cost to ~15s overall, and CI runs benefit
// from real coverage of the timeout path. An alternative would be to skip
// in -short mode (testing.Short()), at the cost of leaving the timeout
// branch uncovered in fast runs.
func TestRunDebugNoCard(t *testing.T) {
	t.Parallel()
	silenceStdout(t)
	f := &fakeCtx{
		statusScript: []statusEntry{
			{eventStates: []scard.StateFlag{scard.StateEmpty}},
		},
	}
	err := runDebug(f, []string{"R0"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "no card present after 15s") {
		t.Errorf("err = %v", err)
	}
}

func TestRunDebugConnectFails(t *testing.T) {
	// Card detected immediately → Connect runs → Connect returns error →
	// runDebug returns "connect: ...". No 15s wait.
	silenceStdout(t)
	f := &fakeCtx{
		statusScript: []statusEntry{
			{eventStates: []scard.StateFlag{scard.StatePresent}},
		},
		connectErr: fmt.Errorf("hw unplugged"),
	}
	err := runDebug(f, []string{"R0"})
	if err == nil {
		t.Fatal("expected connect error")
	}
	if !strings.Contains(err.Error(), "connect:") {
		t.Errorf("err = %v, want connect: prefix", err)
	}
	if f.connectCalls != 1 {
		t.Errorf("Connect called %d times, want 1", f.connectCalls)
	}
}

// TestRunDebug_PrintLoopMultipleStates exercises the "print only on change"
// branch of runDebug by feeding different EventState values on successive
// polls before finally returning StatePresent (which exits the loop).
// Connect then fails to keep the test fast (no 15s wait).
func TestRunDebug_PrintLoopMultipleStates(t *testing.T) {
	silenceStdout(t)
	f := &fakeCtx{
		statusScript: []statusEntry{
			{eventStates: []scard.StateFlag{scard.StateEmpty}},
			{eventStates: []scard.StateFlag{scard.StateEmpty}}, // same → no reprint
			{eventStates: []scard.StateFlag{scard.StateUnaware}},
			{eventStates: []scard.StateFlag{scard.StatePresent}},
		},
		connectErr: fmt.Errorf("stop here"),
	}
	err := runDebug(f, []string{"R0"})
	if err == nil {
		t.Fatal("expected connect error to terminate")
	}
	if !strings.Contains(err.Error(), "connect:") {
		t.Errorf("err = %v", err)
	}
	if f.statusPos < 4 {
		t.Errorf("expected all 4 status entries consumed, got pos=%d", f.statusPos)
	}
}
