# Card identity — slot 2 — 2026-05-11 16:19:02

## Slot status

- status byte: `0x05` (present|processed)

## ATR

- **raw**: `3BD097FF81B1FE451FC7EB`
- convention: direct
- protocols: T=1, T=1, T=15
- TA1=97 → Fi=512, Di=64
- TC1=FF → extra guard time N=255
- TDi=81 → T=1
- TDi=B1 → T=1
- TAi=FE
- TBi=45
- TDi=1F → T=15
- TAi=C7
- historical bytes: none
- TCK: `EB` ✗

## MF-level EFs

| FID  | Name        | SW   | Bytes | Notes |
|------|-------------|------|-------|-------|
| 2F02 | EF.GDO      | 9000 |    12 | global card data object — ICCSN at tag 5A |
| 2F01 | EF.ATR      | 9000 |   134 | SICCT card-info TLV — chip / OS identification |
| 2F00 | EF.DIR      | 6982 |     0 | application directory |
| D080 | EF.Version2 | 6A82 |     0 | gemSpec G2 card version block |
| 2F11 | EF.Version  | 9000 |    40 | legacy version block |

## EF.GDO

- raw: `5A0A80276883110000117594`
- ICCSN (10 bytes): `80276883110000117594`
- MII: 8, country/issuer prefix: 802768

## EF.ATR (TLV)

- tag `E0` (ISO 7816-4 application template) len=17 value=`02020409020300FFFF0202040902020409`
- tag `5F52` (SICCT card capability info) len=12 value=`8066054445545343739621F3` — ".f.DETSCs.!."
- tag `D0` (issuer data) len=3 value=`040300`
- tag `D2` (chip / hardware) len=16 value=`4445494658534C453738313434010000` — "DEIFXSLE78144..."
- tag `D3` (COS / operating system) len=16 value=`545359534954434F5346433230020103` — "TSYSITCOSFC20..."
- tag `D4` (service box / config) len=16 value=`545359534954434F5353423230020300` — "TSYSITCOSSB20..."
- tag `D6` (initialization data) len=16 value=`54535953494944315049303031010000` — "TSYSIID1PI001..."
- tag `CF` (padding / reserved) len=21 value=`000000000000000000000000000000000000000000`

## Application directory probe

| AID                                | Name         | P2=04 SW | P2=0C SW | Notes |
|------------------------------------|--------------|----------|----------|-------|
| D27600000102                       | DF.HCA       | 6A82     | 6A82     | eGK healthcare app (patient master + insurance + protected EFs) |
| A000000167455349474E               | DF.ESIGN     | 9000     | 9000     | ISO/IEC 7816-15 electronic-signature app (HBA / SMC-B / nPA) |
| D27600006601                       | DF.HPA       | 6A82     | 6A82     | HBA Heilberufsausweis profile |
| D27600006602                       | DF.AUTO      | 6A82     | 6A82     | HBA / SMC-B authentication app |
| D27600006603                       | DF.QES       | 6A82     | 6A82     | HBA qualified electronic signature |
| D276000001448000                   | DF.SMA       | 6A82     | 6A82     | SMC-B application DF |
| D2760000148002                     | DF.SAK       | 6A82     | 6A82     | SMC-K Konnektor app (legacy) |
| E828BD080FA000000167455349474E     | DF.CIA.ESIGN | 6A82     | 6A82     | Cryptographic Information Application for ESIGN |
| D27600000144848000                 | DF.NFD       | 6A82     | 6A82     | eGK notfalldaten / emergency-data app (protected) |
| D27600000144830000                 | DF.DPE       | 6A82     | 6A82     | eGK Datenmanagement persönliche Erklärungen (protected) |

## Inferred card class

**T-Systems TCOS test/development card** — DF.ESIGN + TSYSITCOS fingerprint in EF.ATR. Not a production SMC-B; C2C against a real eGK will fail unless this card carries a gematik-chained CV-cert (verify before relying on it).
