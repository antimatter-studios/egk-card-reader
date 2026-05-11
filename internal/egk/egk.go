package egk

import (
	"fmt"
	"os"
)

// CardData bundles everything we extract from the eGK in one read session.
type CardData struct {
	MF        *MFData   // MF-level diagnostics (ICCSN, etc.); nil if unreadable
	Personal  *PersonalData
	Insurance *InsuranceData
	Protected *ProtectedData
	StatusVD  *StatusVD // freshness/state of EF.VD (nil if unreadable)
	RawPD     []byte
	RawVD     []byte
	XMLPD     string // decompressed PD XML
	XMLAVD    string // decompressed AVD XML
	XMLGVD    string // decompressed GVD XML (if present)
	HCAFCP    []byte // FCP/FCI from SELECT DF.HCA, if returned
}

// Read selects the HCA application and reads + parses EF.PD and EF.VD.
//
// Read strategy: SELECT DF.HCA by AID (with FCP), then READ BINARY by SFI to
// avoid relying on FID-based SELECT EF (which fails on some implementations).
// If SFI fails, fall back to SELECT EF by FID.
func Read(card Card) (*CardData, error) {
	// Some cards land in an unexpected DF on power-up; explicitly walk MF first.
	if err := selectMF(card); err != nil {
		// Non-fatal — many eGK cards don't support explicit MF select,
		// they assume MF after reset.
		if os.Getenv("EGK_TRACE") == "1" {
			fmt.Fprintf(os.Stderr, "[apdu] SELECT MF skipped: %v\n", err)
		}
	}

	// Read MF-level diagnostics (ICCSN) before SELECT DF.HCA. After HCA is
	// selected, FID 2F02 means EF.VD, not EF.GDO, so this is the only safe
	// place to grab it. Non-fatal: some cards reject the EF or selectMF was
	// skipped above — diagnostic data is best-effort.
	mf, mfErr := readMF(card)
	if mfErr != nil && os.Getenv("EGK_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[apdu] EF.GDO read skipped: %v\n", mfErr)
	}
	if v, verr := readVersion2(card); verr == nil && v != nil {
		if mf == nil {
			mf = &MFData{}
		}
		mf.Version2 = v
	} else if verr != nil && os.Getenv("EGK_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[apdu] EF.Version2 read skipped: %v\n", verr)
	}

	fcp, err := selectByAID(card, aidHCA)
	if err != nil {
		return nil, fmt.Errorf("select HCA: %w", err)
	}
	ProbeDFHCA(card)
	status, statusErr := readStatusVD(card)
	if statusErr != nil && os.Getenv("EGK_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[apdu] EF.StatusVD read skipped: %v\n", statusErr)
	}

	rawPD, err := readEFCombined(card, sfiPD, fidPD)
	if err != nil {
		return nil, fmt.Errorf("read EF.PD: %w", err)
	}
	pd, pdXML, err := ParsePD(rawPD)
	if err != nil {
		return nil, fmt.Errorf("parse EF.PD: %w", err)
	}

	rawVD, err := readEFCombined(card, sfiVD, fidVD)
	if err != nil {
		return nil, fmt.Errorf("read EF.VD: %w", err)
	}
	vd, gvd, avdXML, gvdXML, err := ParseVD(rawVD)
	if err != nil {
		return nil, fmt.Errorf("parse EF.VD: %w", err)
	}

	return &CardData{
		MF:        mf,
		Personal:  pd,
		Insurance: vd,
		Protected: gvd,
		StatusVD:  status,
		RawPD:     rawPD,
		RawVD:     rawVD,
		XMLPD:     pdXML,
		XMLAVD:    avdXML,
		XMLGVD:    gvdXML,
		HCAFCP:    fcp,
	}, nil
}

// readEFCombined tries SFI access first, falls back to FID-based SELECT EF.
func readEFCombined(card Card, sfi byte, fid uint16) ([]byte, error) {
	data, sfiErr := readEFBySFI(card, sfi, fid)
	if sfiErr == nil {
		return data, nil
	}
	if os.Getenv("EGK_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[apdu] SFI read failed (%v), trying FID select\n", sfiErr)
	}
	if err := selectEF(card, fid); err != nil {
		return nil, fmt.Errorf("SFI failed (%v); FID select failed: %w", sfiErr, err)
	}
	header, err := readBinary(card, 0, 0, 8)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 4096)
	out = append(out, header...)
	for {
		buf, err := readBinary(card, 0, uint16(len(out)), readChunkSize)
		if err != nil {
			return nil, err
		}
		if len(buf) == 0 {
			break
		}
		out = append(out, buf...)
	}
	return out, nil
}
