package document

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/christhomas/card-reader/internal/egk"
)

// RenderFHIR renders a human-readable lipgloss table of every data-bearing
// FHIRPath the encoder in fhir.go emits, so the user can sanity-check the
// parsed eGK data before exporting JSON.
//
// Rows mirror encodeFHIRBundle: Patient (identifier, name, name extensions,
// gender, birthDate, address) plus Coverage (status, type, beneficiary,
// period, payor, identifier/VKNR, GKV extensions).
//
// Empty fields are rendered with an em-dash so the user sees what's missing,
// rather than silently being omitted as the encoder would do.
func RenderFHIR(d *egk.CardData, ik *egk.IKInfo) string {
	var (
		pd egk.PersonalData
		vd egk.InsuranceData
	)
	if d != nil {
		if d.Personal != nil {
			pd = *d.Personal
		}
		if d.Insurance != nil {
			vd = *d.Insurance
		}
	}

	iknr := billingIK(vd)
	insName := billingName(vd)

	// Family-name extensions: only present when name[0]._family.extension is.
	famExtNamen := pd.NamePrefix
	famExtVorsatz := pd.Vorsatzwort

	// Address line + iso21090 line extensions.
	addrLine := strings.TrimSpace(pd.Street + " " + pd.HouseNumber)
	if pd.AddressSuffix != "" && addrLine != "" {
		addrLine = strings.TrimSpace(addrLine + ", " + pd.AddressSuffix)
	}

	// VKNR / Kostenträgergruppe come from KTDA-resolved IKInfo.
	var vknr, ktg string
	if ik != nil {
		vknr = ik.VKNR
		ktg = ik.KostentraegerGruppe
	}

	// Skip BesondereGruppe / DMP placeholder "00" the way the encoder does.
	besondere := vd.BesondereGruppe
	if besondere == "00" {
		besondere = ""
	}
	dmp := vd.DMP
	if dmp == "00" {
		dmp = ""
	}

	rows := []fhirRow{
		// --- Patient ---
		{"Patient.identifier[0].type.coding[0].code", "GKV", "Identifier type — statutory health insurance"},
		{"Patient.identifier[0].system", systemOrDash(pd.InsurantID, "http://fhir.de/sid/gkv/kvid-10"), "GKV KVNR identifier system (de.basisprofil.r4)"},
		{"Patient.identifier[0].value", pd.InsurantID, "KVNR — Krankenversichertennummer (insurant number on the eGK)"},
		{"Patient.name[0].use", "official", "Name use — official registered name"},
		{"Patient.name[0].family", pd.LastName, "Family name (last name)"},
		{"Patient.name[0].given[0]", pd.FirstName, "Given name (first name)"},
		{"Patient.name[0].prefix[0]", pd.Title, "Academic title (e.g. Dr., Prof.)"},
		{"Patient.name[0]._family.extension(namenszusatz)", famExtNamen, "Namenszusatz — nobility/name affix (e.g. Graf, Freiherr)"},
		{"Patient.name[0]._family.extension(own-prefix)", famExtVorsatz, "Vorsatzwort — surname particle (e.g. von, zu, de)"},
		{"Patient.gender", fhirGender(pd.Gender), "Administrative gender (male / female / other / unknown)"},
		{"Patient.birthDate", egk.FormatDate(pd.BirthDate), "Date of birth (YYYY-MM-DD)"},
		{"Patient.address[0].use", addrUse(pd), "Address use — home"},
		{"Patient.address[0].type", addrType(pd), "Address type — postal + physical"},
		{"Patient.address[0].line[0]", addrLine, "Street address line (street + house number + suffix)"},
		{"Patient.address[0]._line[0].extension(streetName)", pd.Street, "iso21090 street name component"},
		{"Patient.address[0]._line[0].extension(houseNumber)", pd.HouseNumber, "iso21090 house-number component"},
		{"Patient.address[0].postalCode", pd.PostalCode, "PLZ — postal code"},
		{"Patient.address[0].city", pd.City, "City / Ort"},
		{"Patient.address[0].country", pd.Country, "Wohnsitzlaendercode — residence country (e.g. D)"},

		// --- Coverage ---
		{"Coverage.status", "active", "Coverage status — assumed active (eGK is in hand)"},
		{"Coverage.type.coding[0].code", "GKV", "Coverage type — statutory (versicherungsart-de-basis)"},
		{"Coverage.beneficiary.reference", patientRef(pd.InsurantID), "Reference to the Patient resource above"},
		{"Coverage.period.start", egk.FormatDate(vd.StartDate), "Versicherungsschutz Beginn — coverage start"},
		{"Coverage.period.end", egk.FormatDate(vd.EndDate), "Versicherungsschutz Ende — coverage end (empty = open)"},
		{"Coverage.payor[0].identifier.system", systemOrDash(iknr, "http://fhir.de/sid/arge-ik/iknr"), "IK identifier system (arge-ik / IKNR)"},
		{"Coverage.payor[0].identifier.value", iknr, "Insurer Institutionskennzeichen (IK) — 9-digit IKNR"},
		{"Coverage.payor[0].display", insName, "Insurer display name (Kasse)"},
		{"Coverage.identifier[0].system", systemOrDash(vknr, "http://fhir.de/sid/gkv/vknr"), "VKNR identifier system (de.basisprofil.r4)"},
		{"Coverage.identifier[0].value", vknr, "VKNR — Vertragskassennummer (5-digit, from KTDA)"},
		{"Coverage.extension(versichertenart).valueCoding.code", vd.InsuredType, "Versichertenart — 1=Member, 3=Family, 5=Pensioner"},
		{"Coverage.extension(besondere-personengruppe).valueCoding.code", besondere, "Besondere Personengruppe (omitted when 00)"},
		{"Coverage.extension(dmp-kennzeichen).valueCoding.code", dmp, "DMP-Kennzeichen — chronic-care marker (omitted when 00)"},
		{"Coverage.extension(wop).valueCoding.code", vd.WOP, "WOP — Wohnortprinzip / KV-region code"},
		{"Coverage.extension(kostentraeger-gruppe).valueCoding.code", ktg, "Kostenträgergruppe (Anlage 6 BMV-Ä; from KTDA)"},
	}

	return renderFHIRTable(rows)
}

type fhirRow struct {
	path, value, meaning string
}

// addrUse / addrType only appear on the Patient when an address dict was
// emitted at all (encoder skips it when street/city/PLZ are all empty).
func addrUse(pd egk.PersonalData) string {
	if pd.Street == "" && pd.City == "" && pd.PostalCode == "" {
		return ""
	}
	return "home"
}
func addrType(pd egk.PersonalData) string {
	if pd.Street == "" && pd.City == "" && pd.PostalCode == "" {
		return ""
	}
	return "both"
}

func patientRef(kvnr string) string {
	if kvnr == "" {
		return "Patient/patient-unknown"
	}
	return "Patient/patient-" + kvnr
}

// systemOrDash returns the FHIR system URL only when the corresponding value
// is non-empty — the encoder won't write an Identifier without its value.
func systemOrDash(value, system string) string {
	if value == "" {
		return ""
	}
	return system
}

// renderFHIRTable mirrors the visual style of glossaryTable in main.go:
// rounded border, bold orange header, dim border lines, cyan-bold first
// column, white value/meaning columns, with a coloured caption above.
func renderFHIRTable(rows []fhirRow) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFB454")).Padding(0, 1)
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true).Padding(0, 1)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6")).Bold(true).Padding(0, 1)
	missValueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7086")).Italic(true).Padding(0, 1)
	meaningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6")).Padding(0, 1)
	captionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).MarginBottom(0)

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70"))).
		Headers("Resource.Path", "Value", "Meaning").
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return headerStyle
			case col == 0:
				return pathStyle
			case col == 1:
				return valueStyle
			default:
				return meaningStyle
			}
		})

	for _, r := range rows {
		val := r.value
		if strings.TrimSpace(val) == "" {
			// Use the dim-italic missing style by rendering the placeholder
			// pre-styled; lipgloss/table preserves embedded ANSI.
			val = missValueStyle.Render("—")
		}
		t.Row(r.path, val, fhirWrap(r.meaning, 60))
	}

	caption := captionStyle.Render("HL7 FHIR R4 Bundle — Patient + Coverage")
	return lipgloss.JoinVertical(lipgloss.Left, caption, t.String())
}

// fhirWrap soft-wraps s at width on word boundaries. Local copy of main.wrap
// since the document package can't import main.
func fhirWrap(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if lipgloss.Width(cur)+1+lipgloss.Width(w) > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	lines = append(lines, cur)
	return strings.Join(lines, "\n")
}
