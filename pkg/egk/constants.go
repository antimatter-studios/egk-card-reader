package egk

// This file is the single source of truth for the magic numbers used to talk
// to the eGK over ISO 7816. The values are not arbitrary — every constant
// here is fixed by a public spec (gemSpec_eGK_ObjSys for FIDs/AIDs,
// ISO 7816-4 for APDU bytes and SW codes, ISO 7816-6 for the well-known TLV
// tags). Defining them once removes the per-call temptation to use a raw
// 0x.. literal and makes call sites readable.

// ----- ISO 7816-4 / ISO 7816-3 APDU header bytes -----
const (
	claISO  byte = 0x00 // CLA — plain ISO 7816-4 (no secure messaging, channel 0)

	insSelect     byte = 0xA4 // SELECT FILE
	insReadBinary byte = 0xB0 // READ BINARY (offset-addressed)
	insReadRecord byte = 0xB2 // READ RECORD (record-addressed)
	insGetResp    byte = 0xC0 // GET RESPONSE — used when card answers with 61xx

	// SELECT FILE P1 values.
	p1SelectMF  byte = 0x00 // select MF (file id 3F00 or empty)
	p1SelectEF  byte = 0x02 // select EF by FID under current DF
	p1SelectAID byte = 0x04 // select DF by AID

	// SELECT FILE P2 values.
	p2NoFCI  byte = 0x0C // do not return file control information
	p2RetFCP byte = 0x04 // return File Control Parameters TLV

	// READ BINARY P1 high bit signals SFI-mode addressing.
	readBinarySFIBit  byte = 0x80
	readBinarySFIMask byte = 0x1F
)

// ----- Status Words (SW1SW2) -----
const (
	sw9000           uint16 = 0x9000 // SUCCESS
	sw61xxHighByte   uint16 = 0x6100 // additional response data available (low byte = length)
	sw6Cxx           uint16 = 0x6C00 // wrong Le; low byte = correct length
	sw6282EndOfFile  uint16 = 0x6282 // end of file reached before Le bytes
	sw6981WrongStruct uint16 = 0x6981 // wrong file structure for command (try a different INS)
	sw6982NotAllowed uint16 = 0x6982 // security status not satisfied — PIN/auth required
	sw6A82FileNotFound uint16 = 0x6A82 // file or application not found
)

// ----- AIDs (application identifiers) per gematik -----
var (
	// DF.HCA — eGK Healthcare Application (the only app on a regular eGK that
	// holds the publicly readable PD/VD data).
	aidHCA = []byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x02}

	// DF.ESIGN — ISO/IEC 7816-15 electronic-signature application (cardholder
	// X.509 certs on HBA/SMC-B/eGK).
	aidESIGN = []byte{0xA0, 0x00, 0x00, 0x01, 0x67, 0x45, 0x53, 0x49, 0x47, 0x4E}
)

// ----- File Identifiers (FIDs) -----
//
// FIDs are scoped to the currently selected DF. A FID like 0x2F02 means
// EF.GDO at MF level but EF.VD inside DF.HCA — order of SELECT matters.

const (
	// Master File.
	fidMF uint16 = 0x3F00

	// MF EFs.
	fidGDO       uint16 = 0x2F02 // EF.GDO — ICCSN (BER-TLV 5A 0A <10>)
	fidVersion2A uint16 = 0xD080 // EF.Version2 — gemSpec_eGK_ObjSys canonical FID
	fidVersion2B uint16 = 0x2F11 // EF.Version2 — legacy / pre-G2.x location

	// DF.HCA EFs (G2.1 layout per gemSpec_eGK_ObjSys §3.4).
	fidPD       uint16 = 0xD001 // EF.PD — Persönliche Versichertendaten (gzipped XML)
	fidVD       uint16 = 0xD002 // EF.VD — Allgemeine + Geschützte Versicherungsdaten
	fidStatusVD uint16 = 0xD00C // EF.StatusVD — VD freshness / update status

	// DF.HCA EFs gated behind PIN.CH / C2C (probed live; all return 6981/6982).
	fidLoggingA uint16 = 0xD003 // candidate — returns 6982 (PIN required)
	fidLoggingB uint16 = 0xD005 // candidate — returns 6982
	fidVerweisA uint16 = 0xD009 // EF.Verweis candidate — returns 6982

	// DF.HCA / DF.ESIGN cert FIDs (sweep range; specific assignments depend
	// on card generation — gemSpec_eGK_ObjSys §3.4 enumerates exact mappings).
	fidCertRangeStart uint16 = 0xC500
	fidCertRangeEnd   uint16 = 0xC50F
)

// efVersion2FIDs is the priority list readVersion2 walks. D080 is the
// gemSpec_eGK_ObjSys §3.4.7 canonical placement; 2F11 covers legacy cards.
var efVersion2FIDs = []uint16{fidVersion2A, fidVersion2B}

// ----- Short File Identifiers (SFIs) inside DF.HCA -----
//
// SFI-addressed READ BINARY skips a SELECT EF round-trip (handy on cards
// where SELECT EF by FID fails). Per gemSpec_eGK_ObjSys G2.1.
const (
	sfiPD byte = 0x01 // EF.PD
	sfiVD byte = 0x02 // EF.VD
)

// ----- BER-TLV tags surfaced by the eGK -----
const (
	tagICCSN          byte = 0x5A // ISO 7816-6 — Card Serial Number (inside EF.GDO)
	tagVersion2Outer  byte = 0xEF // gemSpec — outer wrapper around the C0..C3 children
	tagVersion2ObjSys byte = 0xC0 // gemSpec — first version block (Objektsystem)
	tagVersion2Prod   byte = 0xC1 // gemSpec — Produktidentifikation
	tagVersion2Pers   byte = 0xC2 // gemSpec — Personalisierung (often ASCII-prefixed)
	tagVersion2COS    byte = 0xC3 // gemSpec — Chip Operating System version

	// BER-TLV length-form markers (ISO 7816-4 §5.2.2).
	berLen1 byte = 0x81 // next 1 byte = length
	berLen2 byte = 0x82 // next 2 bytes = length (big-endian)
	berLenLongMarker byte = 0x80 // bit pattern indicating long form
)

// ----- READ BINARY tunables -----
const (
	// readChunkSize bounds each READ BINARY call. 0xFC keeps the response
	// inside a single T=1 IFSC frame (254 bytes IFSC negotiated on G2.x).
	readChunkSize uint16 = 0xFC

	// readBinaryByteOffsetMax — SFI mode encodes the offset in P2 (one byte),
	// so once we pass this we have to fall back to plain (non-SFI) addressing.
	readBinaryByteOffsetMax uint16 = 0xFF
)
