package document

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/antimatter-studios/egk-card-reader/pkg/egk"
)

// RenderGDT returns a lipgloss table rendering of the GDT 2.10 Satzart 6301
// record that would be emitted for d / ik. It is a comprehension view: each
// row shows the 4-digit GDT field code, the raw value as it would appear in
// the file, and a 1-line English description so a user can verify the parse.
//
// Row order mirrors encodeGDT6301 in gdt.go, including the 8000 / 8100 /
// 9218 / 0201 / 0203 / 0205 header fields. 8100 (Satzlänge) is computed the
// same way the encoder computes it — by summing the byte length of every
// emitted line as ISO-8859-15. 4239 (Karte gelesen) is today's date in
// TTMMJJJJ. Empty values render as "—" so missing fields are visible.
func RenderGDT(d *egk.CardData, ik *egk.IKInfo) string {
	var (
		pd egk.PersonalData
		vd egk.InsuranceData
		gv egk.ProtectedData
	)
	if d != nil {
		if d.Personal != nil {
			pd = *d.Personal
		}
		if d.Insurance != nil {
			vd = *d.Insurance
		}
		if d.Protected != nil {
			gv = *d.Protected
		}
	}

	addrLine := strings.TrimSpace(pd.Street + " " + pd.HouseNumber)
	today := time.Now().Format("02012006")

	type row struct{ field, value, meaning string }

	// Build the row list in the exact order encodeGDT6301 emits them. We
	// compute the 8100 length the same way the encoder does: sum of every
	// emitted line's ISO-8859-15 byte length, with an "00000" placeholder
	// for the 8100 line itself (which the encoder later patches in-place
	// without changing length, so the total is stable).
	rows := []row{
		{"8000", "6301", "Record type identifier (Satzart)"},
		{"8100", "", "Total record length in bytes (Satzlänge)"}, // patched below
		{"9218", "02.10", "GDT version"},
		{"0201", "EMPF", "Receiver short-ID (Empfänger-Kürzel)"},
		{"0203", "CRDR", "Sender short-ID (Sender-Kürzel)"},
		{"0205", "card-reader", "Sender software name"},

		// Patient
		{"3000", pd.InsurantID, "Patient ID"},
		{"3101", pd.LastName, "Last name"},
		{"3102", pd.FirstName, "First name(s)"},
		{"3103", gdtDate(pd.BirthDate), "Date of birth (TTMMJJJJ)"},
		{"3104", pd.Title, "Title"},
		{"3105", pd.InsurantID, "Insurance number (KVNR)"},
		{"3106", vd.InsuredType, "Insured-person type (1=Member, 3=Family, 5=Pensioner)"},
		{"3110", gdtSex(pd.Gender), "Sex (1=Male, 2=Female, 3=Indeterminate, 4=Diverse)"},
		{"3112", pd.PostalCode, "Postal code"},
		{"3113", pd.City, "City"},
		{"3114", addrLine, "Street + house number"},
		{"3116", pd.Country, "Country code"},

		// Insurance
		{"4101", billingName(vd), "Health-insurance name"},
		{"4104", billingIK(vd), "Insurer IK (9-digit institution identifier)"},
		{"4108", gdtVKNR(ik), "VKNR (5-digit contract-insurer number)"},
		{"4131", vd.WOP, "WOP (Wohnortprinzip — KV residence region)"},
		{"4133", gdtDate(vd.StartDate), "Insurance start date (TTMMJJJJ)"},
		{"4202", gdtDate(vd.EndDate), "Insurance end date (TTMMJJJJ)"},
		{"4239", today, "Date card was read (TTMMJJJJ, today)"},
	}
	if gv.ZuzahlungStatus == "1" {
		rows = append(rows, row{"4242", gdtDate(gv.ZuzahlungGueltigBis),
			"Co-payment exemption end date (TTMMJJJJ)"})
	}

	// Compute 8100 the same way encodeGDT6301 does — sum of every emitted
	// line's encoded byte length. We skip rows whose value is empty, since
	// the encoder's add() helper drops them.
	total := 0
	for _, r := range rows {
		if r.field == "8100" {
			// placeholder line: "00000" is exactly 5 bytes
			line, err := gdtLine("8100", "00000")
			if err == nil {
				total += len(line)
			}
			continue
		}
		if r.value == "" {
			continue
		}
		line, err := gdtLine(r.field, r.value)
		if err == nil {
			total += len(line)
		}
	}
	for i, r := range rows {
		if r.field == "8100" {
			rows[i].value = fmt.Sprintf("%05d", total)
			break
		}
	}

	// Styles — mirror renderForm / glossaryTable in main.go.
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFB454")).Padding(0, 1)
	fieldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true).Padding(0, 1)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6")).Padding(0, 1)
	missingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7086")).Italic(true).Padding(0, 1)
	meaningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6")).Padding(0, 1)
	captionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))

	// We need to know per-row whether the value is empty so the StyleFunc
	// can dim that cell. Capture by index.
	empties := make(map[int]bool, len(rows))
	for i, r := range rows {
		if r.value == "" {
			empties[i] = true
		}
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70"))).
		Headers("Field", "Raw Value", "Meaning").
		StyleFunc(func(rowIdx, col int) lipgloss.Style {
			switch {
			case rowIdx == table.HeaderRow:
				return headerStyle
			case col == 0:
				return fieldStyle
			case col == 1:
				if empties[rowIdx] {
					return missingStyle
				}
				return valueStyle
			default:
				return meaningStyle
			}
		})
	for _, r := range rows {
		v := r.value
		if v == "" {
			v = "—"
		}
		t.Row(r.field, v, r.meaning)
	}

	caption := captionStyle.Render("GDT 2.10 — Satzart 6301 (Stammdaten)")
	return lipgloss.JoinVertical(lipgloss.Left, caption, t.String())
}
