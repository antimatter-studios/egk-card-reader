package document

import (
	"encoding/json"
	"io"

	"github.com/christhomas/card-reader/internal/egk"
)

// formMappingJSON is the existing --json output: the 21-row form mapping
// rendered as JSON. It's not a clinical-exchange format; it's the same data
// the lipgloss table shows, in a machine-readable shape.
type formMappingJSON struct{}

func (formMappingJSON) Format() string    { return "json" }
func (formMappingJSON) Extension() string { return ".json" }
func (formMappingJSON) Encode(d *egk.CardData, ik *egk.IKInfo) (*Document, error) {
	return captureBytes("json", ".json", func(w io.Writer) error {
		fields := egk.FormMapping(d, ik)
		out := make([]map[string]string, 0, len(fields))
		for _, f := range fields {
			out = append(out, map[string]string{
				"label":  f.Label,
				"value":  f.Value,
				"source": f.Source,
				"note":   f.Note,
			})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	})
}
