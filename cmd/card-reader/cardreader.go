package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ebfe/scard"

	"github.com/christhomas/card-reader/internal/document"
	"github.com/christhomas/card-reader/internal/egk"
	"github.com/christhomas/card-reader/internal/reader"
)

// pcscContext is the subset of *scard.Context that this package needs.
// Defining it lets tests stub PC/SC interactions; *scard.Context satisfies
// it structurally.
type pcscContext interface {
	GetStatusChange([]scard.ReaderState, time.Duration) error
	Connect(reader string, share scard.ShareMode, proto scard.Protocol) (*scard.Card, error)
}

// setupCardReader establishes the PC/SC context and lists readers. The caller
// must call ctx.Release() when done. Only invoked for --input cardreader (and
// --debug); file-based input never touches PC/SC.
func setupCardReader() (*scard.Context, []string, error) {
	ctx, err := scard.EstablishContext()
	if err != nil {
		return nil, nil, fmt.Errorf("PC/SC: cannot establish context: %w", err)
	}
	readers, err := ctx.ListReaders()
	if err != nil {
		ctx.Release()
		return nil, nil, fmt.Errorf("PC/SC: list readers: %w", err)
	}
	if len(readers) == 0 {
		ctx.Release()
		return nil, nil, fmt.Errorf("no PC/SC readers found — plug in your card reader and ensure pcscd is running")
	}
	return ctx, readers, nil
}

// loadCardData fetches CardData from one of:
//   - "cardreader"            → auto-detect (ORGA on /dev/cu.usbmodem*, else PC/SC)
//   - "orga" or "orga:<path>" → force the Ingenico/Worldline ORGA 9xx driver
//   - "pcsc" / "generic"      → force PC/SC (Cherry / OMNIKEY / any CCID reader)
//   - a file path             → parse a previously written .gdt / .hl7 / .fhir.json
//
// For the reader inputs the returned cleanup closes the underlying handles;
// for file input cleanup is nil.
func loadCardData(input string) (*egk.CardData, func(), error) {
	switch {
	case input == "cardreader":
		return loadCardDataAuto()
	case input == "orga" || strings.HasPrefix(input, "orga:"):
		dev := ""
		if strings.HasPrefix(input, "orga:") {
			dev = strings.TrimPrefix(input, "orga:")
		}
		return loadCardDataORGA(dev)
	case input == "pcsc" || input == "generic":
		return loadCardDataPCSC()
	}

	if _, err := os.Stat(input); err != nil {
		return nil, nil, fmt.Errorf("input %q: %w", input, err)
	}
	base := strings.ToLower(filepath.Base(input))
	switch {
	case strings.HasSuffix(base, ".gdt"):
		data, err := document.ParseGDTFile(input)
		if err != nil {
			return nil, nil, fmt.Errorf("parse GDT %s: %w", input, err)
		}
		return data, nil, nil
	case strings.HasSuffix(base, ".fhir.json"):
		data, err := document.ParseFHIRFile(input)
		if err != nil {
			return nil, nil, fmt.Errorf("parse FHIR %s: %w", input, err)
		}
		return data, nil, nil
	case strings.HasSuffix(base, ".hl7"):
		data, err := document.ParseHL7ADTFile(input)
		if err != nil {
			return nil, nil, fmt.Errorf("parse HL7 ADT %s: %w", input, err)
		}
		return data, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported input file %q (supported: .gdt, .hl7, .fhir.json)", filepath.Base(input))
	}
}

// loadCardDataAuto auto-detects the best reader (ORGA via USB VID/PID match,
// else PC/SC) and prints identifying info so the user can confirm which
// device is being used and why.
func loadCardDataAuto() (*egk.CardData, func(), error) {
	probe := reader.Detect()
	pick, err := probe.Pick()
	if err != nil {
		return nil, nil, err
	}
	if pick.Kind == "pcsc" {
		fmt.Fprintln(os.Stderr, "Waiting for card insertion (15s)...")
	}
	return loadCardDataVia(reader.Options{})
}

// loadCardDataPCSC forces the PC/SC path.
func loadCardDataPCSC() (*egk.CardData, func(), error) {
	fmt.Fprintln(os.Stderr, "Waiting for card insertion (15s)...")
	return loadCardDataVia(reader.Options{Force: "generic"})
}

// loadCardDataORGA routes the ORGA path through the reader factory. devNode
// may be empty to auto-detect the first /dev/cu.usbmodem* device.
func loadCardDataORGA(devNode string) (*egk.CardData, func(), error) {
	return loadCardDataVia(reader.Options{Force: "orga", ORGADevNode: devNode})
}

// loadCardDataVia is the common path: open a reader session via the factory,
// print device identification, read the eGK in slot 1, return the parsed
// data plus a cleanup hook.
func loadCardDataVia(opts reader.Options) (*egk.CardData, func(), error) {
	s, err := reader.Open(opts)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintln(os.Stderr, "Reader detected:")
	fmt.Fprintln(os.Stderr, s.Identify())
	cleanup := func() { _ = s.Close() }
	card, err := s.Slot(1)
	if err != nil {
		return nil, cleanup, fmt.Errorf("reader slot 1: %w", err)
	}
	data, err := egk.Read(card)
	if err != nil {
		return nil, cleanup, fmt.Errorf("read eGK: %w", err)
	}
	return data, cleanup, nil
}

// waitForCard does single-flight polling — never overlaps PC/SC calls.
func waitForCard(ctx pcscContext, readers []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		states := make([]scard.ReaderState, len(readers))
		for i, r := range readers {
			states[i].Reader = r
			states[i].CurrentState = scard.StateUnaware
		}
		err := ctx.GetStatusChange(states, 500*time.Millisecond)
		if err != nil && !errors.Is(err, scard.ErrTimeout) {
			return "", err
		}
		for i, s := range states {
			if s.EventState&scard.StatePresent != 0 && s.EventState&scard.StateMute == 0 {
				return readers[i], nil
			}
		}
	}
	return "", fmt.Errorf("no card present after %s", timeout)
}

// suggestBaseName builds "patient-<KVNR>-<timestamp>" or "card-<timestamp>"
// when the card lacks a KVNR. Used as the default basename for output.File.
func suggestBaseName(data *egk.CardData) string {
	id := "card"
	if data != nil && data.Personal != nil && data.Personal.InsurantID != "" {
		id = "patient-" + data.Personal.InsurantID
	}
	return id + "-" + time.Now().Format("20060102-150405")
}

func runDebug(ctx pcscContext, readers []string) error {
	fmt.Println("Readers:")
	for _, r := range readers {
		fmt.Printf("  - %s\n", r)
	}
	fmt.Println()

	deadline := time.Now().Add(15 * time.Second)
	var chosen string
	var lastLine string
	for time.Now().Before(deadline) && chosen == "" {
		states := make([]scard.ReaderState, len(readers))
		for i, r := range readers {
			states[i].Reader = r
			states[i].CurrentState = scard.StateUnaware
		}
		_ = ctx.GetStatusChange(states, 500*time.Millisecond)

		var sb strings.Builder
		for i, s := range states {
			fmt.Fprintf(&sb, "[%s] %s  ATR=%X", readers[i], decodeState(s.EventState), s.Atr)
			if s.EventState&scard.StatePresent != 0 && s.EventState&scard.StateMute == 0 {
				chosen = readers[i]
			}
		}
		line := sb.String()
		if line != lastLine {
			fmt.Println(line)
			lastLine = line
		}
	}
	if chosen == "" {
		return fmt.Errorf("no card present after 15s")
	}
	fmt.Printf("\nCard present in: %s\n\n", chosen)

	card, err := ctx.Connect(chosen, scard.ShareShared, scard.ProtocolAny)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer card.Disconnect(scard.LeaveCard)

	data, err := egk.Read(card)
	if err != nil {
		return fmt.Errorf("read eGK: %w", err)
	}

	fmt.Printf("Raw EF.PD: %d bytes\n", len(data.RawPD))
	fmt.Printf("Raw EF.VD: %d bytes\n", len(data.RawVD))
	fmt.Println()
	fmt.Println("=== EF.PD raw XML ===")
	fmt.Println(data.XMLPD)
	fmt.Println()
	fmt.Println("=== EF.VD AVD raw XML ===")
	fmt.Println(data.XMLAVD)
	if data.XMLGVD != "" {
		fmt.Println()
		fmt.Println("=== EF.VD GVD raw XML ===")
		fmt.Println(data.XMLGVD)
	}
	return nil
}

func decodeState(s scard.StateFlag) string {
	flags := []struct {
		bit  scard.StateFlag
		name string
	}{
		{scard.StateChanged, "CHANGED"},
		{scard.StateUnknown, "UNKNOWN"},
		{scard.StateUnavailable, "UNAVAILABLE"},
		{scard.StateEmpty, "EMPTY"},
		{scard.StatePresent, "PRESENT"},
		{scard.StateAtrmatch, "ATRMATCH"},
		{scard.StateExclusive, "EXCLUSIVE"},
		{scard.StateInuse, "INUSE"},
		{scard.StateMute, "MUTE"},
		{scard.StateUnpowered, "UNPOWERED"},
	}
	var out []string
	for _, f := range flags {
		if f.bit != 0 && s&f.bit != 0 {
			out = append(out, f.name)
		}
	}
	if len(out) == 0 {
		return "NONE"
	}
	return strings.Join(out, "|")
}
