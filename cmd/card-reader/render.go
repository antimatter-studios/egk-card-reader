package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/antimatter-studios/egk-card-reader/pkg/document"
	"github.com/antimatter-studios/egk-card-reader/pkg/egk"
)

// ----- lipgloss styles for the form output -----

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 2)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9DA3B0")).
			Italic(true)

	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1")).Bold(true)
	missStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8")).Bold(true)
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF"))
	valueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6")).Bold(true)
	missValStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7086")).Italic(true)
	sourceStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB454"))
	noteStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")).Italic(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#585B70")).
			Padding(1, 2)

	summaryOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1")).Bold(true)
	summaryMiss = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8")).Bold(true)
)

func renderForm(fields []egk.FormField) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFB454")).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	formTable := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70"))).
		Headers(" ", "Field", "Value", "Source", "Note").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		})

	filled, missing := 0, 0
	for _, f := range fields {
		var mark, value string
		if f.Filled() {
			mark = okStyle.Render("✓")
			value = valueStyle.Render(f.Value)
			filled++
		} else {
			mark = missStyle.Render("✗")
			value = missValStyle.Render("<missing>")
			missing++
		}
		note := ""
		if f.Note != "" {
			note = noteStyle.Render(wrap(f.Note, 60))
		}
		formTable.Row(
			mark,
			labelStyle.Render(f.Label),
			value,
			sourceStyle.Render(f.Source),
			note,
		)
	}

	summary := fmt.Sprintf("%s  %s",
		summaryOK.Render(fmt.Sprintf("✓ %d filled", filled)),
		summaryMiss.Render(fmt.Sprintf("✗ %d missing", missing)),
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		formTable.String(),
		"",
		summary,
	)
}

// renderDiagnostics is a compact card-level diagnostic banner (ICCSN, OS
// version, etc.) rendered below the billing form. Distinct from renderForm so
// the visual separation between billing data and card-identity data is clear.
func renderDiagnostics(fields []egk.FormField) string {
	if len(fields) == 0 {
		return ""
	}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFB454")).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70"))).
		Headers("Diagnostic", "Value", "Source", "Note").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		})
	for _, f := range fields {
		value := valueStyle.Render(f.Value)
		if !f.Filled() {
			value = missValStyle.Render("<missing>")
		}
		note := ""
		if f.Note != "" {
			note = noteStyle.Render(wrap(f.Note, 60))
		}
		t.Row(labelStyle.Render(f.Label), value, sourceStyle.Render(f.Source), note)
	}
	return t.String()
}

// chrome is the title + subtitle banner that sits above every --table output,
// regardless of format. Keeps the visual identity consistent across the form,
// GDT, FHIR, and HL7 ADT views.
func chrome() string {
	title := titleStyle.Render("eGK Card Reader")
	subtitle := subtitleStyle.Render("Read at " + time.Now().Format("2006-01-02 15:04:05"))
	return lipgloss.JoinVertical(lipgloss.Left, title, subtitle, "")
}

// wrap soft-wraps s at width on word boundaries. Used for long notes inside
// table cells so the table doesn't blow out wide.
func wrap(s string, width int) string {
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

// renderTable picks the format-specific lipgloss "comprehension" renderer
// and wraps it in the shared chrome (title + subtitle) so every --table
// output looks the same. The glossary is appended only when --glossary was
// passed — most users don't need the reference tables once they're familiar.
func renderTable(format string, data *egk.CardData, ik *egk.IKInfo, glossary bool) (string, error) {
	var body string
	switch format {
	case "form", "json":
		// json is the form-mapping serialised — same data, same table view.
		body = renderForm(egk.FormMapping(data, ik)) + "\n\n" +
			renderDiagnostics(egk.DiagnosticFields(data))
	case "gdt":
		body = document.RenderGDT(data, ik)
	case "hl7-fhir":
		body = document.RenderFHIR(data, ik)
	case "hl7-adt":
		body = document.RenderHL7ADT(data, ik)
	default:
		return "", fmt.Errorf("no table renderer for --output %s", format)
	}
	parts := []string{body}
	if glossary {
		parts = append(parts, "", renderGlossary())
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...), nil
}
