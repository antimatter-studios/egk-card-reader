package main

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

type glossaryEntry struct{ k, v string }

// renderGlossary builds three reference tables: source-column codes (where
// each value came from on the card), German form labels with their English
// meaning, and general healthcare-system acronyms that appear in the notes.
//
// Acronyms are decomposed instead of repeated: e.g. EF + PD + AVD + GVD
// rather than spelling out EF.PD, EF.AVD, EF.GVD individually.
func renderGlossary() string {
	sources := []glossaryEntry{
		{"EF", "Elementary File — a smart-card data file inside the eGK applications"},
		{"PD", "Persönliche Versichertendaten — personal data (name, DOB, address)"},
		{"AVD", "Allgemeine Versicherungsdaten — general insurance data (insurer, dates)"},
		{"GVD", "Geschützte Versichertendaten — protected insurance data (co-pay, contracts)"},
		{"KTDA", "Kostenträgerdatei — insurer-lookup table from gkv-datenaustausch.de (quarterly)"},
		{"derived", "Computed from other card values (e.g. GKV inferred from card type)"},
		{"today", "Filled with the current date at read time"},
		{"lookup", "Needs an external lookup table not yet integrated"},
		{"practice", "Practice-management config — never on any patient's card"},
	}

	formLabels := []glossaryEntry{
		{"Abrechnung", "Billing scheme (GKV statutory / PKV private)"},
		{"Kasse", "Health insurance fund / insurer"},
		{"IKNR", "9-digit Institution Identification Number"},
		{"KTAB", "Kostenträgerabrechnungsbereich — cost-bearer billing area"},
		{"Kostenträgergruppe", "Cost-bearer group code (Anlage 6 BMV-Ä; 06 = vdek)"},
		{"Karte gelesen", "Date the card was read"},
		{"Versicherungsschutz Beginn", "Coverage start date"},
		{"Versicherungsschutz Ende", "Coverage end date (empty = open-ended)"},
		{"Besondere Personengruppe", "Special insured group (00 = none)"},
		{"Gebührenordnung", "Fee schedule (EBM statutory / GOÄ private)"},
		{"Adresse Teil 1 / 2", "Address line 1 (street) / line 2 (PLZ + city)"},
		{"Betriebsstätte", "Practice site (BSNR)"},
		{"Arzt", "Doctor (LANR)"},
		{"Versicherten-Nr.", "Insurant number (KVNR), printed on the eGK"},
		{"VKNR", "5-digit contract insurer number"},
		{"Bedruckungsname", "Imprint name (Lastname, Firstname)"},
		{"Versichertenart", "Insured category (1 = Member, 3 = Family, 5 = Pensioner)"},
		{"WOP", "Wohnortprinzip — KV residence-region code"},
		{"DMP-Kennzeichen", "Disease-Management-Programme marker (00 = none)"},
		{"gebührenbefreit bis Datum", "Co-payment exemption end date"},
	}

	acronyms := []glossaryEntry{
		{"GKV", "Gesetzliche Krankenversicherung — statutory health insurance"},
		{"PKV", "Private Krankenversicherung — private health insurance"},
		{"eGK", "elektronische Gesundheitskarte — electronic health insurance card"},
		{"EBM", "Einheitlicher Bewertungsmaßstab — uniform fee schedule for statutory billing"},
		{"GOÄ", "Gebührenordnung für Ärzte — fee schedule for private / self-pay billing"},
		{"BMV-Ä", "Bundesmantelvertrag-Ärzte — federal physicians' framework agreement"},
		{"KBV", "Kassenärztliche Bundesvereinigung — national association of statutory-health physicians"},
		{"KV", "Kassenärztliche Vereinigung — regional association of statutory-health physicians"},
		{"BSNR", "Betriebsstättennummer — 9-digit practice-site identifier"},
		{"LANR", "Lebenslange Arztnummer — 9-digit lifetime doctor identifier"},
		{"KVNR", "Krankenversichertennummer — insurant number printed on the eGK"},
		{"IK / IKNR", "Institutionskennzeichen — 9-digit institution number"},
		{"VKNR", "Vertragskassennummer — 5-digit contract insurer number"},
		{"vdek", "Verband der Ersatzkassen — substitute-fund association (TK, Barmer, DAK, KKH, hkk, HEK)"},
		{"AOK", "Allgemeine Ortskrankenkasse — local general sickness fund"},
		{"BKK", "Betriebskrankenkasse — company-based fund"},
		{"IKK", "Innungskrankenkasse — guild-based fund"},
		{"SVLFG / LKK", "Sozialversicherung für Landwirtschaft, Forsten und Gartenbau — agricultural insurer"},
		{"PLZ", "Postleitzahl — postal code"},
		{"DMP", "Disease-Management-Programme — chronic-care programs (diabetes, asthma, COPD…)"},
		{"WOP", "Wohnortprinzip — KV-region residence code"},
		{"VSDM", "Versichertenstammdaten-Management — insurance master-data check"},
	}

	// KTAB values per KBV S_KTS_KTABRECHNUNGSBEREICH_V1.00 — the full set is
	// only 11 codes, all listed for reference.
	ktabValues := []glossaryEntry{
		{"00", "Primärabrechnung — primary billing (default for any normal eGK)"},
		{"01", "Sozialversicherungsabkommen — international social-security agreement billing"},
		{"02", "Bundesversorgungsgesetz — federal war-victims compensation"},
		{"03", "Bundesentschädigungsgesetz — federal compensation law"},
		{"04", "Grenzgänger — cross-border workers"},
		{"05", "Rheinschiffer — Rhine boatmen"},
		{"06", "Sozialhilfeträger — social-aid agencies"},
		{"07", "Bundesvertriebenengesetz — expellees-and-refugees law"},
		{"08", "Asylstelle — asylum-applicant office"},
		{"09", "Schwangerschaftsabbruch — pregnancy-termination billing"},
		{"11", "Wohnausländer — non-resident foreigners"},
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		glossaryTable("Source codes (the [tag] column)", sources),
		"",
		glossaryTable("Form labels (German → English)", formLabels),
		"",
		glossaryTable("KTAB codes (KBV S_KTS_KTABRECHNUNGSBEREICH)", ktabValues),
		"",
		glossaryTable("Healthcare acronyms", acronyms),
	)
}

// glossaryTable builds a 2-column bordered table with a coloured caption.
func glossaryTable(caption string, entries []glossaryEntry) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFB454")).Padding(0, 1)
	termStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true).Padding(0, 1)
	defStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6")).Padding(0, 1)
	captionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).MarginBottom(0)

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70"))).
		Headers("Term", "Meaning").
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return headerStyle
			case col == 0:
				return termStyle
			default:
				return defStyle
			}
		})
	for _, e := range entries {
		t.Row(e.k, wrap(e.v, 80))
	}
	return lipgloss.JoinVertical(lipgloss.Left, captionStyle.Render(caption), t.String())
}
