package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/antimatter-studios/egk-card-reader/pkg/document"
	"github.com/antimatter-studios/egk-card-reader/pkg/output"
)

// longDescription is shown above the auto-generated usage on `--help`. The
// flag-by-flag reference is generated from struct tags on the command types,
// so adding/removing a flag updates the help automatically — no parallel
// helpText constant to keep in sync.
const longDescription = `Read a German eGK (elektronische Gesundheitskarte) and emit
patient/insurance data as a billing-form table or one of several wire formats.

KTDA cadence: re-run "card-reader ktda update" quarterly (1.1., 1.4., 1.7.,
1.10.) to refresh the insurer-lookup table that fills VKNR / Kassenart /
Kostenträgergruppe on the form.

Examples:
  card-reader                                       read card, show GKV form table
  card-reader --output gdt                          read card, show GDT comprehension table
  card-reader --output gdt --file                   read card, write ./output/patient-<KVNR>-<ts>.gdt
  card-reader --input file.gdt                      parse GDT file, show GKV form table
  card-reader --input file.gdt --output hl7-fhir    parse GDT file, show FHIR comprehension table
  card-reader ktda update                           refresh the insurer table

Environment:
  EGK_TRACE=1   log low-level APDU / SFI-fallback chatter to stderr

See also: docs/output-formats.md, README.md`

// CLI is the entire command-line surface. Read is the default command — a
// bare "card-reader" with no subcommand runs it.
type CLI struct {
	Read ReadCmd `cmd:"" default:"withargs" help:"Read a card (or a previously written file) and emit patient/insurance data. Default command — runs when no subcommand is given."`
	Ktda KtdaCmd `cmd:"" help:"Manage the Kostenträgerdatei insurer-lookup table (VKNR / Kassenart / Kostenträgergruppe)."`
}

// ReadCmd is the default command. It either reads a physical card via PC/SC
// or parses a previously written file, then renders or writes the result.
type ReadCmd struct {
	Input  string `placeholder:"SRC" default:"cardreader" help:"Input source: 'cardreader' (PC/SC) or path to a previously written file. Format auto-detected by extension; supported: .gdt, .hl7, .fhir.json. PC/SC is not touched when a file path is given."`
	Output string `placeholder:"FMT" help:"Output format: form | gdt | hl7-fhir | hl7-adt | json. Default: 'form' for cardreader input, or matched to the file extension for file input."`

	Table bool `xor:"dest" help:"Render the comprehension table on stdout (default — verifies parsing in the vocabulary of --output)."`
	File  bool `short:"f" xor:"dest" help:"Write the raw bytes of --output to ./output/<basename>.<ext> instead of rendering a table. Basename: patient-<KVNR>-<ts> (or card-<ts> if KVNR missing). --output form has no byte form."`

	Glossary bool `help:"Append source-code / form-label / KTAB / acronym reference tables under the comprehension table."`
	Debug    bool `short:"d" help:"List readers, watch state changes, dump raw EF.PD/EF.VD XML (cardreader input only)."`
}

// Validate fills the input-dependent default for Output and rejects illegal
// flag combinations. Runs after kong has parsed args and before Run().
func (r *ReadCmd) Validate() error {
	if r.Output == "" {
		if r.Input == "cardreader" {
			r.Output = "form"
		} else {
			out, err := outputForPath(r.Input)
			if err != nil {
				return err
			}
			r.Output = out
		}
	}
	switch r.Output {
	case "form", "gdt", "hl7-fhir", "hl7-adt", "json":
	default:
		return fmt.Errorf("--output: %q is not one of form, gdt, hl7-fhir, hl7-adt, json", r.Output)
	}
	if r.File && r.Output == "form" {
		return fmt.Errorf("--output form has no byte representation; use --table or pick another --output for --file")
	}
	if r.Debug && r.Input != "cardreader" {
		return fmt.Errorf("--debug only works with --input cardreader")
	}
	return nil
}

func (r *ReadCmd) Run() error {
	fmt.Fprintln(os.Stderr, chrome())

	if r.Debug {
		ctx, readers, err := setupCardReader()
		if err != nil {
			return err
		}
		defer ctx.Release()
		return runDebug(ctx, readers)
	}

	data, cleanup, err := loadCardData(r.Input)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	ikInfo := resolveIK(data)

	if r.File {
		encoder, ok := document.Encoders[encoderKey(r.Output)]
		if !ok {
			return fmt.Errorf("no encoder for --output %s", r.Output)
		}
		doc, err := encoder.Encode(data, ikInfo)
		if err != nil {
			return fmt.Errorf("encode %s: %w", r.Output, err)
		}
		w := output.File{Dir: "output", BaseName: suggestBaseName(data)}
		return w.Write(doc)
	}

	out, err := renderTable(r.Output, data, ikInfo, r.Glossary)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// outputForPath maps an input file path to the matching --output value, used
// to auto-default --output when --input is a file. Keep in sync with the
// extensions produced by each Encoder. Detects the compound ".fhir.json" by
// looking at the full basename, since filepath.Ext only returns ".json".
func outputForPath(path string) (string, error) {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(base, ".fhir.json"):
		return "hl7-fhir", nil
	case strings.HasSuffix(base, ".gdt"):
		return "gdt", nil
	case strings.HasSuffix(base, ".hl7"):
		return "hl7-adt", nil
	case strings.HasSuffix(base, ".json"):
		return "json", nil
	}
	return "", fmt.Errorf("cannot infer --output from filename %q (specify --output explicitly)", filepath.Base(path))
}

// encoderKey maps the CLI --output value onto the document.Encoders registry
// key. The CLI uses hyphenated names; the registry uses short identifiers.
func encoderKey(output string) string {
	switch output {
	case "hl7-fhir":
		return "fhir"
	case "hl7-adt":
		return "hl7adt"
	default:
		return output
	}
}
