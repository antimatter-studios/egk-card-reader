package egk

import (
	"fmt"
	"strings"
	"time"
)

// FormField is one row of a German practice-management billing form. Source
// describes where the value comes from so the user can audit auto-fill.
type FormField struct {
	Label  string
	Value  string
	Source string // "EF.PD", "EF.AVD", "EF.GVD", "derived", "today", "KTDA", "practice"
	Note   string // empty for fully resolved; explanation when Value is missing
}

// Filled reports whether the field has a value the form should accept.
func (f FormField) Filled() bool {
	return strings.TrimSpace(f.Value) != ""
}

// IKInfo is the bit of Kostenträgerdatei (KTDA) data we need to resolve
// VKNR / Kassenart / Kostenträgergruppe for the IKNR on the card. The egk
// package stays decoupled from the ktda package — main.go does the lookup
// and hands the resolved values in.
type IKInfo struct {
	Name                string
	VKNR                string
	Kassenart           string
	KostentraegerGruppe string
}

// FormMapping maps a CardData to the standard German GKV billing-form layout
// shown in PVS systems (Tomedo / RED Medical / CGM / etc.). Fields not on
// the eGK are returned with an empty Value and a note explaining why. If
// ikInfo is non-nil, KTDA-derived fields (VKNR, Kostenträgergruppe) get
// auto-filled too.
func FormMapping(d *CardData, ikInfo *IKInfo) []FormField {
	var (
		pd PersonalData
		vd InsuranceData
		gv ProtectedData
	)
	if d.Personal != nil {
		pd = *d.Personal
	}
	if d.Insurance != nil {
		vd = *d.Insurance
	}
	if d.Protected != nil {
		gv = *d.Protected
	}

	// IKNR for billing: Abrechnender_Kostentraeger if present, else Kostentraeger.
	billingIKNR := vd.BillingInsurerID
	billingName := vd.BillingInsurerName
	if billingIKNR == "" {
		billingIKNR = vd.InsurerID
	}
	if billingName == "" {
		billingName = vd.InsurerName
	}

	// Bedruckungsname convention: "Last, First" (without title).
	printedName := strings.TrimSpace(pd.LastName)
	if pd.FirstName != "" {
		if printedName != "" {
			printedName += ", "
		}
		printedName += pd.FirstName
	}

	addr1 := strings.TrimSpace(pd.Street + " " + pd.HouseNumber)
	if pd.AddressSuffix != "" {
		addr1 = strings.TrimSpace(addr1 + ", " + pd.AddressSuffix)
	}
	addr2 := strings.TrimSpace(pd.PostalCode + " " + pd.City)

	// Default codes per GKV billing where a card omits the field.
	besondere := vd.BesondereGruppe
	if besondere == "" {
		besondere = "00" // 00 = keine besondere Personengruppe (Defaultwert)
	}
	dmp := vd.DMP
	if dmp == "" {
		dmp = "00" // 00 = kein DMP-Kennzeichen (Defaultwert)
	}

	// Co-pay (Zuzahlung) maps from GVD: Status=1 means exempt, Gültig_bis is the date.
	copayUntil := ""
	if gv.ZuzahlungStatus == "1" {
		copayUntil = FormatDate(gv.ZuzahlungGueltigBis)
	}

	today := time.Now().Format("2006-01-02")

	return []FormField{
		// ---- left column ----
		{Label: "Abrechnung", Value: "GKV", Source: "derived",
			Note: "Statutory health card → GKV billing"},
		{Label: "Kasse", Value: billingName, Source: "EF.AVD"},
		{Label: "IKNR", Value: billingIKNR, Source: "EF.AVD"},
		{Label: "KTAB", Value: ktabFromIKInfo(ikInfo), Source: ktabSource(ikInfo),
			Note: ktabNote(ikInfo)},
		{Label: "Kostenträgergruppe", Value: ktgValue(ikInfo), Source: ktgSource(ikInfo),
			Note: ktgNote(ikInfo)},
		{Label: "Karte gelesen", Value: today, Source: "today"},
		{Label: "Versicherungsschutz Beginn", Value: FormatDate(vd.StartDate), Source: "EF.AVD"},
		{Label: "Besondere Personengruppe", Value: besondere, Source: "EF.AVD"},
		{Label: "Gebührenordnung", Value: "", Source: "practice",
			Note: "Practice-level setting (e.g. EBM/GOÄ); not on eGK."},
		{Label: "Adresse Teil 1", Value: addr1, Source: "EF.PD"},
		{Label: "Adresse Teil 2", Value: addr2, Source: "EF.PD"},

		// ---- right column ----
		{Label: "Betriebsstätte", Value: "", Source: "practice",
			Note: "Practice-level (BSNR); not on eGK."},
		{Label: "Arzt", Value: "", Source: "practice",
			Note: "Practice-level (LANR); not on eGK."},
		{Label: "Versicherten-Nr.", Value: pd.InsurantID, Source: "EF.PD"},
		{Label: "VKNR", Value: vknrValue(ikInfo), Source: vknrSource(ikInfo),
			Note: vknrNote(ikInfo)},
		{Label: "Bedruckungsname", Value: printedName, Source: "EF.PD"},
		{Label: "Versichertenart", Value: vd.InsuredType, Source: "EF.AVD",
			Note: explainInsuredType(vd.InsuredType)},
		{Label: "WOP", Value: vd.WOP, Source: "EF.AVD",
			Note: explainWOP(vd.WOP)},
		{Label: "DMP-Kennzeichen", Value: dmp, Source: "EF.AVD"},
		{Label: "Versicherungsschutz Ende", Value: FormatDate(vd.EndDate), Source: "EF.AVD"},
		{Label: "gebührenbefreit bis Datum", Value: copayUntil, Source: "EF.GVD",
			Note: copayNote(gv.ZuzahlungStatus, gv.ZuzahlungGueltigBis)},
		{Label: "Selektivvertrag (ärztlich)", Value: gv.SelektivAerztlich, Source: "EF.GVD",
			Note: explainSelektiv(gv.SelektivAerztlich)},
		{Label: "Selektivvertrag (zahnärztlich)", Value: gv.SelektivZahnaerztlich, Source: "EF.GVD",
			Note: explainSelektiv(gv.SelektivZahnaerztlich)},
	}
}

// DiagnosticFields returns card-level diagnostic info — ICCSN, OS/objsys
// version, status — distinct from the billing-form fields in FormMapping.
// These come from MF and DF.HCA admin EFs (no PIN required) and are useful
// for debugging / support, not for clinical or billing output.
func DiagnosticFields(d *CardData) []FormField {
	var fields []FormField
	if d == nil {
		return []FormField{{
			Label:  "ICCSN",
			Value:  "",
			Source: "MF.EF.GDO",
			Note:   "Card serial not read (selectMF/EF.GDO failed or skipped).",
		}}
	}
	if d.MF != nil {
		fields = append(fields, FormField{
			Label:  "ICCSN",
			Value:  d.MF.ICCSN,
			Source: "MF.EF.GDO",
			Note:   "Integrated Circuit Card Serial Number — uniquely identifies the physical chip.",
		})
	} else {
		fields = append(fields, FormField{
			Label:  "ICCSN",
			Value:  "",
			Source: "MF.EF.GDO",
			Note:   "Card serial not read (selectMF/EF.GDO failed or skipped).",
		})
	}
	if s := d.StatusVD; s != nil {
		ts := s.Timestamp
		if ts == "" {
			ts = "(unparsed)"
		}
		fields = append(fields, FormField{
			Label:  "VD letzte Aktualisierung",
			Value:  ts,
			Source: "DF.HCA.EF.StatusVD",
			Note:   "Timestamp at which the insurer last refreshed EF.VD on this card.",
		})
		if s.StatusHex != "" {
			fields = append(fields, FormField{
				Label:  "VD-Status (raw)",
				Value:  s.StatusHex,
				Source: "DF.HCA.EF.StatusVD",
				Note:   "Trailing status bytes; layout per gemSpec_eGK_ObjSys.",
			})
		}
	}
	if d.MF != nil && d.MF.Version2 != nil {
		v := d.MF.Version2
		fields = append(fields,
			FormField{Label: "Version2 / C0", Value: v.TagC0, Source: "MF.EF.Version2",
				Note: "First version block — gemSpec_eGK_ObjSys / Objektsystem on most cards."},
			FormField{Label: "Version2 / C1", Value: v.TagC1, Source: "MF.EF.Version2",
				Note: "Second version block — Produktidentifikation."},
			FormField{Label: "Version2 / C2", Value: v.TagC2, Source: "MF.EF.Version2",
				Note: "Version der Personalisierung (often carries manufacturer string)."},
			FormField{Label: "Version2 / C3", Value: v.TagC3, Source: "MF.EF.Version2",
				Note: "COS / chip OS version block; empty on cards that don't emit it."},
		)
	}
	return fields
}

// explainSelektiv decodes the Aerztlich/Zahnaerztlich code per gemSpec_eGK_Fach.
// The field is a single digit (Integer 0..9). 0 means no participation; any
// non-zero digit means the cardholder participates in at least one selective
// contract (the exact contract identity isn't on the card — only the flag).
func explainSelektiv(s string) string {
	switch {
	case s == "":
		return "not present on card"
	case s == "0":
		return "0 = keine Teilnahme an Selektivverträgen"
	case len(s) == 1 && s[0] >= '1' && s[0] <= '9':
		return s + " = Teilnahme an Selektivvertrag (Code per Kassenkonfiguration)"
	}
	return ""
}

func explainInsuredType(s string) string {
	switch s {
	case "1":
		return "1 = Mitglied"
	case "3":
		return "3 = Familienversicherter"
	case "5":
		return "5 = Rentner"
	}
	return ""
}

func explainWOP(s string) string {
	if s == "" {
		return ""
	}
	// KBV WOP table — common values.
	wop := map[string]string{
		"01": "Schleswig-Holstein", "02": "Hamburg", "03": "Bremen", "17": "Niedersachsen",
		"20": "Westfalen-Lippe", "38": "Nordrhein", "46": "Hessen", "51": "Rheinland-Pfalz",
		"52": "Baden-Württemberg", "71": "Bayern", "72": "Berlin", "73": "Saarland",
		"78": "Mecklenburg-Vorpommern", "83": "Brandenburg", "88": "Sachsen-Anhalt",
		"93": "Thüringen", "98": "Sachsen",
	}
	if name, ok := wop[s]; ok {
		return s + " = " + name
	}
	return ""
}

// ----- KTDA-resolved field helpers -----
//
// Each comes in three variants: value, source label, and explanatory note.
// They all degrade gracefully when ikInfo is nil (KTDA not loaded).

func vknrValue(ik *IKInfo) string {
	if ik == nil {
		return ""
	}
	return ik.VKNR
}
func vknrSource(ik *IKInfo) string {
	if ik != nil && ik.VKNR != "" {
		return "KTDA"
	}
	return "lookup"
}
func vknrNote(ik *IKInfo) string {
	if ik == nil {
		return "Vertragskassennummer — run `card-reader ktda update` to enable auto-fill."
	}
	if ik.VKNR == "" {
		return "VKNR not present in KTDA for this IKNR (some IKs omit it)."
	}
	return ""
}

func ktgValue(ik *IKInfo) string {
	if ik == nil {
		return ""
	}
	return ik.KostentraegerGruppe
}
func ktgSource(ik *IKInfo) string {
	if ik != nil && ik.KostentraegerGruppe != "" {
		return "KTDA"
	}
	return "lookup"
}
func ktgNote(ik *IKInfo) string {
	if ik == nil {
		return "Derived from Kassenart in KTDA — run `card-reader ktda update`."
	}
	if ik.KostentraegerGruppe != "" {
		return "Derived from Kassenart " + ik.Kassenart + " (KBV Anlage 6)."
	}
	return "Kassenart not resolvable from KTDA."
}

// KTAB (Kostenträgerabrechnungsbereich) per KBV S_KTS_KTABRECHNUNGSBEREICH:
// only 11 public codes exist. For any normal eGK (which is what we read here)
// the answer is always "00" — Primärabrechnung. The other codes apply only
// to special billing constellations that wouldn't be represented on a regular
// patient's eGK at all (asylum-case billing, BVG/BEG compensation cases,
// Schwangerschaftsabbruch, Sozialhilfe-Leistungserbringer, etc.).
func ktabFromIKInfo(_ *IKInfo) string { return "00" }
func ktabSource(_ *IKInfo) string    { return "derived" }
func ktabNote(_ *IKInfo) string {
	return "00 = Primärabrechnung — the default for any patient holding a regular GKV eGK. Override only for special cases (BVG, asylum, refugee, cross-border worker, etc.)."
}

func copayNote(status, until string) string {
	switch status {
	case "":
		return "Co-pay status not on card → assume not exempt"
	case "0":
		return "Status 0 = nicht zuzahlungsbefreit (no exemption)"
	case "1":
		if until == "" {
			return "Status 1 = zuzahlungsbefreit (exempt) — end date missing"
		}
		return "Status 1 = zuzahlungsbefreit until " + FormatDate(until)
	}
	return fmt.Sprintf("Status code %q (unknown)", status)
}
