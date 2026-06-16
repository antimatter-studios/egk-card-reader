package document

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/christhomas/card-reader/pkg/egk"
)

// fhirEncoder is the Encoder registered as "fhir".
type fhirEncoder struct{}

func (fhirEncoder) Format() string    { return "fhir" }
func (fhirEncoder) Extension() string { return ".fhir.json" }
func (e fhirEncoder) Encode(d *egk.CardData, ik *egk.IKInfo) (*Document, error) {
	return captureBytes("fhir", ".fhir.json", func(w io.Writer) error {
		return encodeFHIRBundle(d, ik, w)
	})
}

// encodeFHIRBundle writes an HL7 FHIR R4 Bundle (collection) holding one
// Patient and one Coverage resource to w as pretty-printed JSON.
//
// Identifier systems and extension URLs follow de.basisprofil.r4 conventions
// (KVNR, IKNR, German GKV codings). The output is structurally valid base R4
// but does NOT assert KBV_PR_FOR_* profile conformance — see
// docs/output-formats.md for the rationale and what changes a strict-KBV
// variant would need.
func encodeFHIRBundle(d *egk.CardData, ik *egk.IKInfo, w io.Writer) error {
	if d == nil {
		return fmt.Errorf("nil card data")
	}
	var (
		pd egk.PersonalData
		vd egk.InsuranceData
	)
	if d.Personal != nil {
		pd = *d.Personal
	}
	if d.Insurance != nil {
		vd = *d.Insurance
	}

	patientID := fhirPatientID(pd.InsurantID)
	coverageID := fhirCoverageID(pd.InsurantID, billingIK(vd))

	bundle := map[string]any{
		"resourceType": "Bundle",
		"type":         "collection",
		"timestamp":    time.Now().Format(time.RFC3339),
		"entry": []any{
			map[string]any{
				"fullUrl":  "Patient/" + patientID,
				"resource": fhirPatient(patientID, &pd),
			},
			map[string]any{
				"fullUrl":  "Coverage/" + coverageID,
				"resource": fhirCoverage(coverageID, patientID, &vd, ik),
			},
		},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(bundle)
}

func fhirPatientID(kvnr string) string {
	if kvnr == "" {
		return "patient-unknown"
	}
	return "patient-" + kvnr
}

func fhirCoverageID(kvnr, iknr string) string {
	switch {
	case kvnr != "" && iknr != "":
		return "coverage-" + kvnr + "-" + iknr
	case kvnr != "":
		return "coverage-" + kvnr
	default:
		return "coverage-unknown"
	}
}

func fhirPatient(id string, pd *egk.PersonalData) map[string]any {
	p := map[string]any{
		"resourceType": "Patient",
		"id":           id,
	}

	if pd.InsurantID != "" {
		p["identifier"] = []any{
			map[string]any{
				"type": map[string]any{
					"coding": []any{
						map[string]any{
							"system": "http://fhir.de/CodeSystem/identifier-type-de-basis",
							"code":   "GKV",
						},
					},
				},
				"system": "http://fhir.de/sid/gkv/kvid-10",
				"value":  pd.InsurantID,
			},
		}
	}

	name := map[string]any{
		"use":    "official",
		"family": pd.LastName,
	}
	if pd.FirstName != "" {
		name["given"] = []any{pd.FirstName}
	}
	if pd.Title != "" {
		name["prefix"] = []any{pd.Title}
	}
	var familyExts []any
	if pd.NamePrefix != "" {
		familyExts = append(familyExts, map[string]any{
			"url":         "http://fhir.de/StructureDefinition/humanname-namenszusatz",
			"valueString": pd.NamePrefix,
		})
	}
	if pd.Vorsatzwort != "" {
		familyExts = append(familyExts, map[string]any{
			"url":         "http://hl7.org/fhir/StructureDefinition/humanname-own-prefix",
			"valueString": pd.Vorsatzwort,
		})
	}
	if len(familyExts) > 0 {
		name["_family"] = map[string]any{"extension": familyExts}
	}
	p["name"] = []any{name}

	if g := fhirGender(pd.Gender); g != "" {
		p["gender"] = g
	}
	if pd.BirthDate != "" {
		p["birthDate"] = egk.FormatDate(pd.BirthDate)
	}

	if addr := fhirAddress(pd); addr != nil {
		p["address"] = []any{addr}
	}

	return p
}

func fhirGender(s string) string {
	switch strings.ToUpper(s) {
	case "M":
		return "male"
	case "W", "F":
		return "female"
	case "X":
		return "unknown"
	case "D":
		return "other"
	}
	return ""
}

func fhirAddress(pd *egk.PersonalData) map[string]any {
	if pd.Street == "" && pd.City == "" && pd.PostalCode == "" {
		return nil
	}
	addr := map[string]any{
		"type": "both",
		"use":  "home",
	}
	line := strings.TrimSpace(pd.Street + " " + pd.HouseNumber)
	if pd.AddressSuffix != "" && line != "" {
		line = strings.TrimSpace(line + ", " + pd.AddressSuffix)
	}
	if line != "" {
		addr["line"] = []any{line}
		var lineExts []any
		if pd.Street != "" {
			lineExts = append(lineExts, map[string]any{
				"url":         "http://hl7.org/fhir/StructureDefinition/iso21090-ADXP-streetName",
				"valueString": pd.Street,
			})
		}
		if pd.HouseNumber != "" {
			lineExts = append(lineExts, map[string]any{
				"url":         "http://hl7.org/fhir/StructureDefinition/iso21090-ADXP-houseNumber",
				"valueString": pd.HouseNumber,
			})
		}
		if len(lineExts) > 0 {
			addr["_line"] = []any{
				map[string]any{"extension": lineExts},
			}
		}
	}
	if pd.City != "" {
		addr["city"] = pd.City
	}
	if pd.PostalCode != "" {
		addr["postalCode"] = pd.PostalCode
	}
	if pd.Country != "" {
		addr["country"] = pd.Country
	}
	return addr
}

func fhirCoverage(id, patientID string, vd *egk.InsuranceData, ik *egk.IKInfo) map[string]any {
	c := map[string]any{
		"resourceType": "Coverage",
		"id":           id,
		"status":       "active",
		"type": map[string]any{
			"coding": []any{
				map[string]any{
					"system": "http://fhir.de/CodeSystem/versicherungsart-de-basis",
					"code":   "GKV",
				},
			},
		},
		"beneficiary": map[string]any{
			"reference": "Patient/" + patientID,
		},
	}

	period := map[string]any{}
	if vd.StartDate != "" {
		period["start"] = egk.FormatDate(vd.StartDate)
	}
	if vd.EndDate != "" {
		period["end"] = egk.FormatDate(vd.EndDate)
	}
	if len(period) > 0 {
		c["period"] = period
	}

	payor := map[string]any{}
	if iknr := billingIK(*vd); iknr != "" {
		payor["identifier"] = map[string]any{
			"system": "http://fhir.de/sid/arge-ik/iknr",
			"value":  iknr,
		}
	}
	if name := billingName(*vd); name != "" {
		payor["display"] = name
	}
	c["payor"] = []any{payor}

	if ik != nil && ik.VKNR != "" {
		c["identifier"] = []any{
			map[string]any{
				"system": "http://fhir.de/sid/gkv/vknr",
				"value":  ik.VKNR,
			},
		}
	}

	var exts []any
	if vd.InsuredType != "" {
		exts = append(exts, fhirCodingExtension(
			"http://fhir.de/StructureDefinition/gkv/versichertenart",
			"http://fhir.de/CodeSystem/gkv/versichertenart",
			vd.InsuredType,
		))
	}
	if vd.BesondereGruppe != "" && vd.BesondereGruppe != "00" {
		exts = append(exts, fhirCodingExtension(
			"http://fhir.de/StructureDefinition/gkv/besondere-personengruppe",
			"http://fhir.de/CodeSystem/gkv/besondere-personengruppe",
			vd.BesondereGruppe,
		))
	}
	if vd.DMP != "" && vd.DMP != "00" {
		exts = append(exts, fhirCodingExtension(
			"http://fhir.de/StructureDefinition/gkv/dmp-kennzeichen",
			"http://fhir.de/CodeSystem/gkv/dmp-kennzeichen",
			vd.DMP,
		))
	}
	if vd.WOP != "" {
		exts = append(exts, fhirCodingExtension(
			"http://fhir.de/StructureDefinition/gkv/wop",
			"http://fhir.de/CodeSystem/gkv/wop",
			vd.WOP,
		))
	}
	if ik != nil && ik.KostentraegerGruppe != "" {
		exts = append(exts, fhirCodingExtension(
			"http://fhir.de/StructureDefinition/gkv/kostentraeger-gruppe",
			"http://fhir.de/CodeSystem/gkv/kostentraeger-gruppe",
			ik.KostentraegerGruppe,
		))
	}
	if len(exts) > 0 {
		c["extension"] = exts
	}

	return c
}

func fhirCodingExtension(url, codeSystem, code string) map[string]any {
	return map[string]any{
		"url": url,
		"valueCoding": map[string]any{
			"system": codeSystem,
			"code":   code,
		},
	}
}
