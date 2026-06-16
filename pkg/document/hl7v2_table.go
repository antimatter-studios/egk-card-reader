package document

// hl7v2_table.go renders a "comprehension view" of the HL7 v2.5 ADT^A04
// message that hl7v2.go would emit for a given CardData. The output is a
// lipgloss table with three columns — Segment.Field, Value, Meaning — so
// a clinician or integration engineer can eyeball-verify the parse before
// piping the raw HL7 to a downstream system.
//
// The set of rows MUST mirror what encodeADTA04 actually produces. Whenever
// hl7v2.go grows or shrinks a field, this table needs the matching row.

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/antimatter-studios/egk-card-reader/pkg/egk"
)

// hl7Row is one component-level row in the comprehension table.
type hl7Row struct {
	field   string // e.g. "PID-5.1"
	value   string // populated value or "" → rendered as "—"
	meaning string // 1-line plain-English description
}

// RenderHL7ADT returns a lipgloss-rendered table summarising every populated
// component of every segment that encodeADTA04 emits (MSH/EVN/PID/PV1/IN1).
// Empty components render as "—" so missing fields are visually distinct.
func RenderHL7ADT(d *egk.CardData, ik *egk.IKInfo) string {
	var (
		pd egk.PersonalData
		vd egk.InsuranceData
	)
	if d != nil && d.Personal != nil {
		pd = *d.Personal
	}
	if d != nil && d.Insurance != nil {
		vd = *d.Insurance
	}

	now := time.Now().Format("20060102150405")
	msgID := "CR" + now

	// Reproduce a few derived values exactly as hl7v2.go does so the table
	// matches the wire bytes byte-for-byte.
	billIK := billingIK(vd)
	billName := billingName(vd)
	pidAssigner := "GKV"
	if billIK != "" {
		pidAssigner = "GKV&" + billIK + "&IKNR"
	}
	rows := []hl7Row{
		// --- MSH: Message Header ---
		{"MSH-1", "|", "Field Separator"},
		{"MSH-2", "^~\\&", "Encoding Characters (component, repetition, escape, sub-component)"},
		{"MSH-3.1", "CARD-READER", "Sending Application"},
		{"MSH-4.1", "PRACTICE", "Sending Facility"},
		{"MSH-5.1", "PVS", "Receiving Application (Praxisverwaltungssystem)"},
		{"MSH-6.1", "PRACTICE", "Receiving Facility"},
		{"MSH-7.1", now, "Date/Time of Message (YYYYMMDDHHMMSS)"},
		{"MSH-8", "", "Security"},
		{"MSH-9.1", "ADT", "Message Code"},
		{"MSH-9.2", "A04", "Trigger Event — Register a Patient"},
		{"MSH-9.3", "ADT_A01", "Message Structure"},
		{"MSH-10", msgID, "Message Control ID (unique per message)"},
		{"MSH-11", "P", "Processing ID — P = Production"},
		{"MSH-12", "2.5", "Version ID — HL7 v2.5"},
		{"MSH-18", "UNICODE UTF-8", "Character Set (lets German umlauts travel literally)"},

		// --- EVN: Event Type ---
		{"EVN-1", "A04", "Event Type Code — Register a Patient"},
		{"EVN-2", now, "Recorded Date/Time"},

		// --- PID: Patient Identification ---
		{"PID-1", "1", "Set ID — PID"},
		{"PID-3.1", pd.InsurantID, "Patient Identifier (KVNR — insurant number)"},
		{"PID-3.4", pidAssigner, "Assigning Authority (GKV; sub-components: namespace&IK&IKNR)"},
		{"PID-3.5", "MR", "Identifier Type Code — MR = Medical Record number"},
		{"PID-5.1", pd.LastName, "Family Name"},
		{"PID-5.2", pd.FirstName, "Given Name"},
		{"PID-5.5", pd.Title, "Prefix / academic title (e.g. Dr.)"},
		{"PID-7", condDate(pd.BirthDate), "Date/Time of Birth (YYYYMMDD)"},
		{"PID-8", hl7Sex(pd.Gender), "Administrative Sex (M/F/U/A)"},
		{"PID-11.1", strings.TrimSpace(pd.Street + " " + pd.HouseNumber), "Street Address (street + house number)"},
		{"PID-11.2", pd.AddressSuffix, "Other Designation (Anschriftenzusatz)"},
		{"PID-11.3", pd.City, "City"},
		{"PID-11.5", pd.PostalCode, "Postal Code (PLZ)"},
		{"PID-11.6", pd.Country, "Country"},

		// --- PV1: Patient Visit ---
		{"PV1-1", "1", "Set ID — PV1"},
		{"PV1-2", "O", "Patient Class — O = Outpatient"},

		// --- IN1: Insurance ---
		{"IN1-1", "1", "Set ID — IN1"},
		{"IN1-2.1", "GKV", "Insurance Plan ID (Gesetzliche Krankenversicherung)"},
		{"IN1-3.1", billIK, "Insurance Company ID (IKNR — 9-digit institution number)"},
		{"IN1-3.4", "DE-IK", "Assigning Authority — DE-IK namespace"},
		{"IN1-3.5", "XX", "Identifier Type Code — XX = Organization ID"},
		{"IN1-4.1", billName, "Insurance Company Name"},
		{"IN1-8", hl7VKNR(ik), "Group Number — VKNR (5-digit contract insurer number)"},
		{"IN1-12", condDate(vd.StartDate), "Plan Effective Date (Versicherungsschutz Beginn)"},
		{"IN1-13", condDate(vd.EndDate), "Plan Expiration Date (Versicherungsschutz Ende)"},
		{"IN1-15", vd.InsuredType, "Plan Type — repurposed for Versichertenart (1=Member, 3=Family, 5=Pensioner)"},
		{"IN1-16.1", pd.LastName, "Name of Insured — Family Name"},
		{"IN1-16.2", pd.FirstName, "Name of Insured — Given Name"},
		{"IN1-16.5", pd.Title, "Name of Insured — Prefix / Title"},
		{"IN1-17", "SEL", "Insured's Relationship to Patient — SEL = Self"},
	}

	// Styles mirroring renderForm / glossaryTable in main.go.
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFB454")).Padding(0, 1)
	termStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true).Padding(0, 1)
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6")).Padding(0, 1)
	missStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7086")).Italic(true).Padding(0, 1)
	meaningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")).Padding(0, 1)
	captionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFB454"))

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70"))).
		Headers("Segment.Field", "Value", "Meaning").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch col {
			case 0:
				return termStyle
			case 1:
				// Value column — caller provides pre-styled text via Row(),
				// so just give it padding here.
				return lipgloss.NewStyle().Padding(0, 1)
			default:
				return meaningStyle
			}
		})

	for _, r := range rows {
		var v string
		if r.value == "" {
			v = missStyle.Render("—")
		} else {
			v = valStyle.Render(r.value)
		}
		t.Row(r.field, v, r.meaning)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		captionStyle.Render("HL7 v2.5 ADT^A04 — Register a Patient"),
		t.String(),
	)
}
