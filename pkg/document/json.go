package document

import (
	"encoding/json"
	"io"

	"github.com/christhomas/card-reader/pkg/egk"
)

// formMappingJSON is the --json output: the form mapping + card diagnostics
// rendered as machine-readable JSON. Shape:
//
//	{
//	  "form":        [{label, value, source, note}, ...],   // billing-form rows
//	  "diagnostics": [{label, value, source, note}, ...]    // ICCSN, OS version, ...
//	}
//
// Not a clinical-exchange format — it's the same data the lipgloss table
// shows, structured for downstream tools.
type formMappingJSON struct{}

func (formMappingJSON) Format() string    { return "json" }
func (formMappingJSON) Extension() string { return ".json" }
func (formMappingJSON) Encode(d *egk.CardData, ik *egk.IKInfo) (*Document, error) {
	return captureBytes("json", ".json", func(w io.Writer) error {
		toRows := func(fields []egk.FormField) []map[string]string {
			out := make([]map[string]string, 0, len(fields))
			for _, f := range fields {
				out = append(out, map[string]string{
					"label":  f.Label,
					"value":  f.Value,
					"source": f.Source,
					"note":   f.Note,
				})
			}
			return out
		}
		payload := map[string]any{
			"form":        toRows(egk.FormMapping(d, ik)),
			"diagnostics": toRows(egk.DiagnosticFields(d)),
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	})
}
