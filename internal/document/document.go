// Package document encodes a parsed eGK CardData into one of several
// clinical-exchange document formats (FHIR, HL7 v2 ADT, GDT, plain JSON).
//
// The package owns *what* the document looks like; it does not own *where*
// the document goes — that's the output package's job. An Encoder produces a
// Document (bytes + metadata), and any Writer in the output package can
// persist it. New formats are added by implementing Encoder and registering
// in Encoders.
package document

import (
	"bytes"
	"io"

	"github.com/christhomas/card-reader/internal/egk"
)

// Document is the result of encoding card data in a specific format.
// It carries the encoded bytes plus the metadata (format name + filename
// extension) needed to persist or label them — no destination knowledge.
type Document struct {
	Format    string // short identifier matching Encoder.Format(), e.g. "fhir"
	Extension string // filename extension including the dot, e.g. ".fhir.json"
	Bytes     []byte
}

// Encoder converts a CardData into a Document for one specific output format.
// Implementations live alongside their format helpers (fhir.go, hl7v2.go,
// gdt.go, json.go) and register themselves in Encoders.
type Encoder interface {
	Format() string
	Extension() string
	Encode(d *egk.CardData, ik *egk.IKInfo) (*Document, error)
}

// Encoders is the registry of all supported output formats. Lookup by the
// short name used in CLI flags ("fhir", "hl7adt", "gdt", "json"). Adding a
// new format = implement Encoder + add a line here.
var Encoders = map[string]Encoder{
	"json":   formMappingJSON{},
	"fhir":   fhirEncoder{},
	"hl7adt": hl7v2ADTEncoder{},
	"gdt":    gdtEncoder{},
}

// captureBytes runs an io.Writer-style encode function and wraps the
// captured output in a Document. Encoders use this so their actual logic
// stays in streaming form (good for tests, large outputs) while the public
// API stays Document-shaped.
func captureBytes(format, ext string, f func(io.Writer) error) (*Document, error) {
	var buf bytes.Buffer
	if err := f(&buf); err != nil {
		return nil, err
	}
	return &Document{
		Format:    format,
		Extension: ext,
		Bytes:     buf.Bytes(),
	}, nil
}

// billingIK returns the IK that should be used for billing — the
// AbrechnenderKostentraeger.IK if present, else the issuing-insurer IK.
// Used by every encoder, so it lives here rather than in each one.
func billingIK(vd egk.InsuranceData) string {
	if vd.BillingInsurerID != "" {
		return vd.BillingInsurerID
	}
	return vd.InsurerID
}

// billingName mirrors billingIK for the insurer's display name.
func billingName(vd egk.InsuranceData) string {
	if vd.BillingInsurerName != "" {
		return vd.BillingInsurerName
	}
	return vd.InsurerName
}
