package document

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/christhomas/card-reader/internal/egk"
)

// ParseFHIR reads a FHIR R4 Bundle (collection) JSON document — the format
// produced by encodeFHIRBundle — and reconstructs a *egk.CardData populated
// with Personal and Insurance only. RawPD/RawVD/XMLPD/XMLAVD/XMLGVD/HCAFCP
// have no representation in FHIR and are left zero.
//
// The implementation is deliberately tolerant: missing extensions fall back to
// plain values (e.g. address line is split on the last space if no
// streetName/houseNumber extension is present), unknown extension URLs are
// ignored, and unrecognised resourceTypes in entries are skipped.
func ParseFHIR(r io.Reader) (*egk.CardData, error) {
	if r == nil {
		return nil, fmt.Errorf("nil reader")
	}
	var bundle fhirBundle
	dec := json.NewDecoder(r)
	if err := dec.Decode(&bundle); err != nil {
		return nil, fmt.Errorf("decode FHIR bundle: %w", err)
	}

	cd := &egk.CardData{}
	for _, entry := range bundle.Entry {
		if len(entry.Resource) == 0 {
			continue
		}
		// Peek at resourceType.
		var head struct {
			ResourceType string `json:"resourceType"`
		}
		if err := json.Unmarshal(entry.Resource, &head); err != nil {
			continue
		}
		switch head.ResourceType {
		case "Patient":
			var p fhirPatientResource
			if err := json.Unmarshal(entry.Resource, &p); err == nil {
				cd.Personal = patientToPersonal(&p)
			}
		case "Coverage":
			var c fhirCoverageResource
			if err := json.Unmarshal(entry.Resource, &c); err == nil {
				cd.Insurance = coverageToInsurance(&c)
			}
		default:
			// Unknown — skip.
		}
	}
	return cd, nil
}

// ParseFHIRFile opens path and delegates to ParseFHIR.
func ParseFHIRFile(path string) (*egk.CardData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return ParseFHIR(f)
}

// --- minimal FHIR struct subset --------------------------------------------

type fhirBundle struct {
	ResourceType string          `json:"resourceType"`
	Type         string          `json:"type"`
	Entry        []fhirBundleEnt `json:"entry"`
}

type fhirBundleEnt struct {
	FullURL  string          `json:"fullUrl"`
	Resource json.RawMessage `json:"resource"`
}

type fhirIdentifier struct {
	System string `json:"system"`
	Value  string `json:"value"`
}

type fhirExtension struct {
	URL         string          `json:"url"`
	ValueString string          `json:"valueString"`
	ValueCoding *fhirCoding     `json:"valueCoding,omitempty"`
	Extension   []fhirExtension `json:"extension,omitempty"`
}

type fhirCoding struct {
	System string `json:"system"`
	Code   string `json:"code"`
}

type fhirElement struct {
	Extension []fhirExtension `json:"extension"`
}

type fhirHumanName struct {
	Use     string       `json:"use"`
	Family  string       `json:"family"`
	Given   []string     `json:"given"`
	Prefix  []string     `json:"prefix"`
	Family_ *fhirElement `json:"_family,omitempty"`
}

type fhirAddressR struct {
	Type       string         `json:"type"`
	Use        string         `json:"use"`
	Line       []string       `json:"line"`
	Line_      []*fhirElement `json:"_line,omitempty"`
	City       string         `json:"city"`
	PostalCode string         `json:"postalCode"`
	Country    string         `json:"country"`
}

type fhirPatientResource struct {
	ResourceType string           `json:"resourceType"`
	ID           string           `json:"id"`
	Identifier   []fhirIdentifier `json:"identifier"`
	Name         []fhirHumanName  `json:"name"`
	Gender       string           `json:"gender"`
	BirthDate    string           `json:"birthDate"`
	Address      []fhirAddressR   `json:"address"`
}

type fhirReference struct {
	Reference  string          `json:"reference"`
	Display    string          `json:"display"`
	Identifier *fhirIdentifier `json:"identifier,omitempty"`
}

type fhirPeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type fhirCoverageResource struct {
	ResourceType string           `json:"resourceType"`
	ID           string           `json:"id"`
	Status       string           `json:"status"`
	Identifier   []fhirIdentifier `json:"identifier"`
	Period       *fhirPeriod      `json:"period,omitempty"`
	Payor        []fhirReference  `json:"payor"`
	Extension    []fhirExtension  `json:"extension"`
}

// --- mapping ---------------------------------------------------------------

func patientToPersonal(p *fhirPatientResource) *egk.PersonalData {
	pd := &egk.PersonalData{}

	for _, id := range p.Identifier {
		if id.System == "http://fhir.de/sid/gkv/kvid-10" && id.Value != "" {
			pd.InsurantID = id.Value
			break
		}
	}

	if len(p.Name) > 0 {
		n := p.Name[0]
		pd.LastName = n.Family
		if len(n.Given) > 0 {
			pd.FirstName = n.Given[0]
		}
		if len(n.Prefix) > 0 {
			pd.Title = n.Prefix[0]
		}
		if n.Family_ != nil {
			for _, ext := range n.Family_.Extension {
				url := strings.ToLower(ext.URL)
				switch {
				case strings.Contains(url, "namenszusatz"):
					pd.NamePrefix = ext.ValueString
				case strings.Contains(url, "own-prefix"):
					pd.Vorsatzwort = ext.ValueString
				}
			}
		}
	}

	pd.Gender = parseFHIRGender(p.Gender)
	pd.BirthDate = stripDashes(p.BirthDate)

	if len(p.Address) > 0 {
		a := p.Address[0]
		pd.City = a.City
		pd.PostalCode = a.PostalCode
		pd.Country = a.Country

		var (
			extStreet string
			extHouse  string
			haveExt   bool
		)
		if len(a.Line_) > 0 && a.Line_[0] != nil {
			for _, ext := range a.Line_[0].Extension {
				url := strings.ToLower(ext.URL)
				switch {
				case strings.Contains(url, "streetname"):
					extStreet = ext.ValueString
					haveExt = true
				case strings.Contains(url, "housenumber"):
					extHouse = ext.ValueString
					haveExt = true
				}
			}
		}
		if haveExt {
			pd.Street = extStreet
			pd.HouseNumber = extHouse
		} else if len(a.Line) > 0 {
			line := a.Line[0]
			if idx := strings.LastIndex(line, " "); idx >= 0 {
				pd.Street = line[:idx]
				pd.HouseNumber = line[idx+1:]
			} else {
				pd.Street = line
			}
		}
	}

	return pd
}

func coverageToInsurance(c *fhirCoverageResource) *egk.InsuranceData {
	vd := &egk.InsuranceData{}

	if len(c.Payor) > 0 {
		pay := c.Payor[0]
		if pay.Identifier != nil && pay.Identifier.Value != "" {
			vd.InsurerID = pay.Identifier.Value
			vd.BillingInsurerID = pay.Identifier.Value
		}
		if pay.Display != "" {
			vd.InsurerName = pay.Display
			vd.BillingInsurerName = pay.Display
		}
	}

	if c.Period != nil {
		vd.StartDate = stripDashes(c.Period.Start)
		vd.EndDate = stripDashes(c.Period.End)
	}

	for _, ext := range c.Extension {
		url := strings.ToLower(ext.URL)
		val := extensionCode(ext)
		if val == "" {
			continue
		}
		switch {
		case strings.Contains(url, "versichertenart"):
			vd.InsuredType = val
		case strings.Contains(url, "wop"):
			vd.WOP = val
		case strings.Contains(url, "besondere-personengruppe"):
			vd.BesondereGruppe = val
		case strings.Contains(url, "dmp-kennzeichen"):
			vd.DMP = val
		default:
			// kostentraeger-gruppe and unknown URLs — skip.
		}
	}

	// Coverage-level identifier (vknr) is metadata for IKInfo; ignore.

	return vd
}

func extensionCode(ext fhirExtension) string {
	if ext.ValueCoding != nil && ext.ValueCoding.Code != "" {
		return ext.ValueCoding.Code
	}
	return ext.ValueString
}

// parseFHIRGender is the inverse of fhirGender in fhir.go.
func parseFHIRGender(s string) string {
	switch strings.ToLower(s) {
	case "male":
		return "M"
	case "female":
		return "W"
	case "other":
		return "D"
	case "unknown":
		return "X"
	}
	return ""
}

// stripDashes turns "YYYY-MM-DD" into "YYYYMMDD". Returns the input unchanged
// if the shape doesn't match so we don't silently corrupt non-date strings.
func stripDashes(s string) string {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return s
	}
	return s[0:4] + s[5:7] + s[8:10]
}
