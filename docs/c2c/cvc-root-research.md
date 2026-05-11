# gematik CVC-Root research

Authoritative log of the gematik **CVC PKI** root trust-anchors used for
card-to-card (C2C) authentication on the German eGK / SMC-B / HBA
(gemSpec_COS chapter 13, gemSpec_PKI, gemSpec_CVC_Root).

This document is the *trust-research* deliverable; embedding the bytes
into `internal/c2c/keys/cvc_roots.go` is a follow-up step.

Fetched: 2026-05-11. All artifacts are publicly available from the Atos /
Eviden CVC-Root TSP web GUI, which is the operator approved by gematik
for the CVC-Root product per `gemProdT_CVC_Root_ECC`.

## TL;DR

- The gematik CVC-Root PKI has **two parallel hierarchies**, both
  operated by Atos / Eviden under a gematik approval:
  - **PU** (Produktiv / production) — CHR prefix `DEZGW…`, 1st through
    7th generation, current active root is `DEZGW870226`.
  - **RU/TU** (Referenz-Umgebung / Test-Umgebung — single hierarchy
    shared by both gematik test environments) — CHR prefix `DEGXX…`,
    1st through 8th generation, current active root is `DEGXX890225`.
- All 15 root certificates ever issued **are Brainpool P-256r1 +
  ECDSA-SHA-256**. No RSA, no P-384, no P-512 has ever been used at the
  root tier. (Sub-CAs and end-entity CVCs likewise use P-256r1 in TI;
  P-384r1 and P-512r1 are only allowed by gemSpec_Krypt, not currently
  used by the CVC-Root operator.)
- The role OID in every CHA of every root is `1.2.276.0.76.4.152`
  (`id-CVC-Root-CA`, BSI / gematik OID arc).
- Each root is **self-signed** (CAR == CHR). Generation rollover is
  performed via Cross/Link CVCs (each new root is also issued as a CVC
  signed by the previous root, and the previous root is also issued as a
  CVC signed by the new root, so cards trusting *either* generation can
  bootstrap to the other; gemSpec_CVC_Root §3 Cross-Zertifikat).

## Source of truth

| What                     | URL                                                                                     |
| ------------------------ | ---------------------------------------------------------------------------------------- |
| Operator portal (GUI)    | https://pki.atos.net/egk (redirects to https://cvc.egk-tsp.de.atos.net/)                |
| Listing API (JSON)       | https://cvc.egk-tsp.de.atos.net/api/service/cvc                                          |
| Per-file download (GET)  | https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/{INT_ID}/{INT_ID}_{slot}        |
|                          | where slot 1 = `*.cvc` (CVC), 2 = `*_pubkey.cvc` (raw 7F49 SPKI), 3 = `*.cvc.pdf` (human-readable) |
| Fachportal product page  | https://fachportal.gematik.de/hersteller-anbieter/komponenten-dienste/cvc-root           |
| Product-type spec        | https://gemspec.gematik.de/docs/gemProdT/gemProdT_CVC_Root_ECC/                          |
| gemSpec_CVC_Root v1.9.1  | https://gemspec.gematik.de/downloads/gemSpec/gemSpec_CVC_Root/gemSpec_CVC_Root_V1.9.1.pdf |
| gemSpec_PKI (current)    | https://gemspec.gematik.de/docs/gemSpec/gemSpec_PKI/                                     |
| gemSpec_Krypt v2.29.0    | https://gemspec.gematik.de/downloads/gemSpec/gemSpec_Krypt/gemSpec_Krypt_V2.29.0_Aend.html |
| Reference Java parser    | https://github.com/gematik/lib-smartcards (`de.gematik.smartcards.g2icc.cvc.TrustCenter`) |

**Important:** the spec PDFs (`gemSpec_PKI`, `gemSpec_CVC_Root`,
`gemSpec_CVC_TSP`) describe the *format and process* but do NOT embed
the bytes of the root keys. The bytes themselves are published only on
the operator portal above. The chain of trust to take a key as
authoritative is:

1. gematik publishes the operator approval on `fachportal.gematik.de`
   (Eviden Germany GmbH, version 1.3.2, approved 2023-06-29).
2. That operator (Atos / Eviden) publishes the CVC-Root keys on
   `https://pki.atos.net/egk` / `https://cvc.egk-tsp.de.atos.net/` as
   per gemSpec_CVC_Root §5.1 ("Der Anbieter der CVC-Root-CA
   veröffentlicht den aktuellen öffentlichen Schlüssel der
   CVC-Root-CA").
3. TLS to that operator portal terminates with a publicly trusted
   X.509 cert; integrity below that depends on TLS plus operator
   custody. The portal does *not* publish a detached signature, and the
   FINGERPRINT column displayed in the GUI does *not* match the SHA-256
   of the cert DER nor of the SubjectPublicKeyInfo (semantic of that
   value is undocumented; treat as informational only and use the
   SHA-256(cert_DER) values below as the canonical pinning hash).

## Format of each root CVC

Every root is a 220-byte BSI TR-03110 §C.1 CV-certificate:

```
7F 21 81 D8                              # outer wrapper, len 216
  7F 4E 81 91                            # body, len 145
    5F 29 01 70                          # CPI = 0x70 (gematik root profile)
    42 08 <CAR>                          # 8-byte Certificate Authority Reference
    7F 49 4D                             # 77-byte SubjectPublicKeyInfo
      06 08 2A 86 48 CE 3D 04 03 02      # OID 1.2.840.10045.4.3.2 = ecdsa-with-SHA256
      86 41 04 <X 32 bytes> <Y 32 bytes> # uncompressed point on brainpoolP256r1
    5F 20 08 <CHR>                       # 8-byte Certificate Holder Reference (== CAR)
    7F 4C 13                             # CHA (Certificate Holder Authorisation)
      06 08 2A 82 14 00 4C 04 81 18      # OID 1.2.276.0.76.4.152 = id-CVC-Root-CA
      53 07 FF FF FF FF FF FF FF         # Discretionary data / role bits = root
    5F 25 06 <YYMMDD nibble-BCD>         # NotBefore (CED)
    5F 24 06 <YYMMDD nibble-BCD>         # NotAfter (CXD)
  5F 37 40 <r 32 bytes> <s 32 bytes>     # raw ECDSA signature over body
```

The body bytes covered by the signature are exactly the inner 145 bytes
between offsets 8 and 153 of the wrapper (i.e. everything inside the
`7F 4E 81 91 ... ` envelope but excluding that envelope's tag+length).

The 8-byte CAR/CHR is `C ASCII × 5 + 3 binary bytes`, where the three
binary bytes are `0x87 0xXX 0xYY` (PU) or `0x88 0xXX 0xYY` (RU/TU) and
encode the discriminator (1 nibble) + month (2 nibbles) + year (2
nibbles); decoded form e.g. `DEZGW870226` = `DEZGW` + `87` + `02` + `26`
i.e. "DEZGW, ID 7, valid from 2026-02".

## Production roots (PU)  —  CHR prefix `DEZGW`

### 1st Root — DEZGW810214 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664358605780/1664358605780_1` |
| Self-signed      | yes (CAR == CHR == `DEZGW810214`) |
| Size             | 220 bytes |
| SHA-256 (cert)   | `6a857dae2fee2e2e1c3852a97b6e24ae63f4a49e0dee6876459db72e620fd57b` |
| SHA-256 (body)   | `d23c2d8e5f8668f48dc250f212f0f0b90b5e2957b7e645befbdddd77aa1f762d` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Role OID         | `1.2.276.0.76.4.152` (id-CVC-Root-CA) |
| Validity         | 2014-07-08 .. 2024-07-07 |
| Public key X     | `88c2f43eb17fbf0689296f0bf25f2ad71fad0022edb3711365ec71b83ee26b7e` |
| Public key Y     | `286782fede14b239afea69383069fc112ed2237eeb1758cc60f037143c1da8d2` |

### 2nd Root — DEZGW820216 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664358712897/1664358712897_1` |
| Self-signed      | yes |
| SHA-256 (cert)   | `3072049c75b036761667d9bb23a364ee064f9f17e9a08db9e9ed08763981a9b6` |
| SHA-256 (body)   | `9a9d3eb20da0bd86f851851ade5b5cd3f63b570b30ef64dfc53c7d3c32deb442` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Validity         | 2016-06-07 .. 2026-06-06 |
| Public key X     | `a42ee03e1e077b5db4dc347d3e22ce02ac3f44f0ad583ecb2f57e69ec96089da` |
| Public key Y     | `78b619056e17932fe64b1b41e21c05ee546d2909dc357e35612e1a2479c10d55` |

### 3rd Root — DEZGW830218 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664359044207/1664359044207_1` |
| SHA-256 (cert)   | `987f850bb85210efd4522a479b6d8c3f7e083710c036035b0af131cb64f199b9` |
| SHA-256 (body)   | `ce761fc7fc6cc7a52aa2c72c18a339616446f21f605c451213fac98a5d2e6fbd` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Validity         | 2018-05-23 .. 2028-05-22 |
| Public key X     | `7c807128394d17dea746b55ee26d993ad3fb1bac7b649ce5da9af265c2bf1515` |
| Public key Y     | `598a37b9f9f9347151cecbf9756caa03b2866ee7ea220b4df80028f08f779bd5` |

### 4th Root — DEZGW840220 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664359884820/1664359884820_1` |
| SHA-256 (cert)   | `a4db82dba0f19564eeeb0de3bbd4328b1ab5df0d2fb103598e5b29a9f58b8bba` |
| SHA-256 (body)   | `ba6c67305da47f52292bb67c19aba6af1aa8dd79c29dfd438ed9add6bec90832` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Validity         | 2020-05-13 .. 2030-05-12 |
| Public key X     | `00c6ebb5b22db0b11bbc632e173b94f42d81e90d8a1b0d6d73bda38974065164` |
| Public key Y     | `9a694ade782bd577b8bec6dffad37e14503599896bf4e7a739a34d9d8d783a3e` |

### 5th Root — DEZGW850222 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664360183513/1664360183513_1` |
| SHA-256 (cert)   | `e8da75e6639bb80e6edcc02dc004d16a3ce8617014ca78c6025a4d7bda0d2bfb` |
| SHA-256 (body)   | `0d911cd765ca17c8e4b2e64e64e865afef8d7678c65f4417c8122516c00580c3` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Validity         | 2022-05-05 .. 2032-05-04 |
| Public key X     | `62df8742130fed6b8258aef76627aba1bc8f0d35122d7a09992518cd05f6318e` |
| Public key Y     | `9d7d89bbcc60e5b4608292c1b3fbda89ee8a3406069f038c6722c8d5aad5f2d3` |

### 6th Root — DEZGW860224 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1714134554340/1714134554340_1` |
| SHA-256 (cert)   | `381e300a15266486c611b6042e3928f555634ca07d9679ebec1026a70762236a` |
| SHA-256 (body)   | `777c9c55755a7a94f08705a309b4cecee1ed41919ca21f0ca419c0f977a3dc95` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Validity         | 2024-04-24 .. 2034-04-23 |
| Public key X     | `9ec5add782e97c0852e88520226cb6b72ccbbc67c41ae6d16f2c1e128b90d0a4` |
| Public key Y     | `1fcfb5257da3e3c6889196bc68eb66a12f3c36f902049ba2dffe3889beaa979e` |

### 7th Root — DEZGW870226 — **Aktiv** (current PRODUCTION trust-anchor)

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1776410492215/1776410492215_1` |
| Self-signed      | yes |
| Size             | 220 bytes |
| SHA-256 (cert)   | `364d3a85fad1dbca9c4d8e1a2b9d07c61e4343abafe79dae38563887cf604a98` |
| SHA-256 (body)   | `8ed2b8ebead9cf9cd1562c1643d7ac20950cc2547c3853c6cc9e205e562a4b4d` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Role OID         | `1.2.276.0.76.4.152` (id-CVC-Root-CA) |
| Validity         | **2026-04-15 .. 2036-04-14** |
| Public key X     | `10ae4964dc71ecd41f937a303ce302e84a3305e7f9606cedfecae0d99983bc8c` |
| Public key Y     | `7c9ce8a2d46141bbdc83ed356f2cf6534ca2473ca676e0bbae69129bac6a3d6e` |
| Cross-cert       | also issued as a Link-CVC signed by the 6th Root (DEZGW860224); fetch from INT_ID 1776410578677 |

#### Full DER bytes (copy-paste into Go)

```
// DEZGW870226 — gematik production CVC-Root 7, active 2026-04-15..2036-04-14
// 220 bytes; sha256 = 364d3a85fad1dbca9c4d8e1a2b9d07c61e4343abafe79dae38563887cf604a98
const cvcRootProdActiveHex = "" +
    "7f2181d87f4e81915f2901704208" +
    "44455a4757870226" + // CAR "DEZGW" \x87\x02\x26
    "7f494d06082a8648ce3d04030286" +
    "4104" +
    "10ae4964dc71ecd41f937a303ce302e84a3305e7f9606cedfecae0d99983bc8c" + // X
    "7c9ce8a2d46141bbdc83ed356f2cf6534ca2473ca676e0bbae69129bac6a3d6e" + // Y
    "5f2008" +
    "44455a4757870226" + // CHR
    "7f4c1306082a8214004c0481185307ffffffffffffff" + // CHA: role 1.2.276.0.76.4.152
    "5f2506020600040105" + // CED 2026-04-15
    "5f2406030600040104" + // CXD 2036-04-14
    "5f3740" +
    "8a1e5cf60acc5a18879ef524362cbe8cdebc813d3ca6531b8221107fdbf9d5e0" + // r
    "1f3640f6fe7993da4f40b5db9d03c2505dcd12de9b2e65e7d94dcb0ded264bf6"   // s
```

## TEST roots (RU/TU)  —  CHR prefix `DEGXX`

The gematik *RU* and *TU* environments share a single CVC-Root hierarchy
operated by Atos / Eviden in the same portal. These are clearly
distinguished from production by the CHR prefix `DEGXX` (vs `DEZGW`)
and the disjoint serial space. **They MUST NOT be configured as a
production trust anchor.**

### 1st Root — DEGXX820214 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664355527887/1664355527887_1` |
| SHA-256 (cert)   | `60e148ffa34e8934bbd8869987351fe3e82fe24e33fb819694c1969bb93b4d7c` |
| SHA-256 (body)   | `05d9a99ba72b9fc9a9ae7d68aa5046ac1a5b8126f47948fa6e16e0d0dfbe4d16` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Validity         | 2014-02-27 .. 2024-02-26 |
| Public key X     | `8534d8887f3c7ec18b50b91f09ef3979e86a6f4fc314f3a91ddcc0d271c8c2fd` |
| Public key Y     | `66f9399d7ad8de5fc7dc09435f2130585b6e7ed4fb2f599f5aea4b4b15a44a40` |

### 2nd Root — DEGXX830214 — Test der Cross-Zertifikate

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664356514329/1664356514329_1` |
| SHA-256 (cert)   | `bbaf46a8b261543062a0cfd529af5edaa74409375254ff498434551099990671` |
| SHA-256 (body)   | `63c912bb320ff65b828199d0a5c541939cb64a3e4849ed8c932afbd9b40ee0e2` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Validity         | 2014-09-23 .. 2024-09-22 |
| Public key X     | `0e33b79383171d098f140e3f9336ce25d450afacdcda30c97ebc74a2c60680d2` |
| Public key Y     | `8d89aad9b4418c62fab1481d7485df045e09e03e87368d1a283145bf46b8f6bb` |

### 3rd Root — DEGXX840216 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664356941227/1664356941227_1` |
| SHA-256 (cert)   | `0baadf5c3a4f3a16b530c9554da65fd7e1fa4e0d787c0cd4fab862ea3601bf08` |
| SHA-256 (body)   | `36d83c410d50d3e2859ddea6a2d285eea4eacd459383dd73328c23eb5744c17a` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Validity         | 2016-02-24 .. 2026-02-23 |
| Public key X     | `41faf966ccdf8f58a0765be0d9d169464b73a14a4a7090e20090d3f64ddb1adb` |
| Public key Y     | `4e5fdad143bd9dfef8fc1092900770549eafb92ac7a1ed6804cd6aeefc22dc75` |

### 4th Root — DEGXX850218 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664357453111/1664357453111_1` |
| SHA-256 (cert)   | `2d2892ff3621c7b70825eb8921a3b4538024589a66813f5df36185bf8d6a072c` |
| SHA-256 (body)   | `da07945cd3842fb9388072e564a16532d4eee8e186c9470b33e46c8690c0d3f3` |
| Validity         | 2018-02-06 .. 2028-02-05 |
| Public key X     | `8cb4f87557094d804563f099bfe8525dbb6383a28deb2882f61e1b4267d2dfca` |
| Public key Y     | `392317436b86c05110fd318c627957a9766967d2ba1159ac84276d2bf38f8bae` |

### 5th Root — DEGXX860220 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664357767882/1664357767882_1` |
| SHA-256 (cert)   | `5e44d17a12d6edf07d973e05ea1c302c626a93f3a987407322cec72b72a45888` |
| SHA-256 (body)   | `6a1b02111fb4c039813769a939b2b6d01603afea0e106499991a436f77da6ba9` |
| Validity         | 2020-01-22 .. 2030-01-21 |
| Public key X     | `5b89575b2776601df3312e1cde0d1b0f020b5841706affe947f8c70776a2deb4` |
| Public key Y     | `3e7589041b87397a5ae69efb494747ddd2ef68f2ae68f7fb02f2df5c07819c10` |

### 6th Root — DEGXX870222 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1664358085028/1664358085028_1` |
| SHA-256 (cert)   | `bf183cdaa843b00f272daf2ba71a895f385d633fb19e73e7b46fdfcdabb657d9` |
| SHA-256 (body)   | `c2cf83f5a2c3e389d3a383cae5d30970cb4425bd32603f843cadbb3e43b41c78` |
| Validity         | 2022-01-19 .. 2032-01-18 |
| Public key X     | `015497bb49f7f3a639179ad2d4d6943bb10dbec480cd3c213d8012f71065d557` |
| Public key Y     | `9420d6fa4129aa67e04033507dc9bef283775f66dd470bf6498a3d23f35f27e1` |

### 7th Root — DEGXX880224 — Inaktiv

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1705926940362/1705926940362_1` |
| SHA-256 (cert)   | `36bf5c3aed6345aad84dc0ad12da2c3b1dac0456c3f1f4ffcb66804286880621` |
| SHA-256 (body)   | `94df08b93488d043b9f6e6b972a18e1195447b85217b86b84037935adb067fbf` |
| Validity         | 2024-01-11 .. 2034-01-10 |
| Public key X     | `2881fc603f2a4f9ffa47d4950d33d85955a3a12de9a918c6201fa800433a76f1` |
| Public key Y     | `9fb6007a58e060376dcdee721081cc13a546ac6b4d02fd00ec16f6c9bef92de1` |

### 8th Root — DEGXX890225 — **Aktiv** (current TEST trust-anchor)

| Field            | Value |
| ---------------- | ----- |
| File             | `https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/1766027554972/1766027554972_1` |
| Self-signed      | yes |
| Size             | 220 bytes |
| SHA-256 (cert)   | `417d4bbfcf58a33ecce65fb513d7a2c0735106baa0e91a269865edd275db40f0` |
| SHA-256 (body)   | `37d05d4f20bd92cb039bce21e20a38bd6f6ad67ec6ea35437fc7b7d3941c0994` |
| Algorithm        | ECDSA / brainpoolP256r1 / SHA-256 |
| Role OID         | `1.2.276.0.76.4.152` (id-CVC-Root-CA) |
| Validity         | **2025-12-10 .. 2035-12-09** |
| Public key X     | `124d7746f3bf7b739976d28c7adb4d12d31bd665f1c00b4e48de3ade0e7eb31e` |
| Public key Y     | `325f2aa2d590076279c4300e88dc0f9fad1972eabf4a9b327eed3f76723bdd5c` |
| Cross-cert       | also issued as a Link-CVC signed by the 7th Root (DEGXX880224); INT_ID 1766027930581 |

#### Full DER bytes (copy-paste into Go)

```
// DEGXX890225 — gematik RU/TU CVC-Root 8, active 2025-12-10..2035-12-09
// TEST PKI; rejected by production cards. 220 bytes;
// sha256 = 417d4bbfcf58a33ecce65fb513d7a2c0735106baa0e91a269865edd275db40f0
const cvcRootTestActiveHex = "" +
    "7f2181d87f4e81915f2901704208" +
    "4445475858890225" + // CAR "DEGXX" \x89\x02\x25
    "7f494d06082a8648ce3d04030286" +
    "4104" +
    "124d7746f3bf7b739976d28c7adb4d12d31bd665f1c00b4e48de3ade0e7eb31e" + // X
    "325f2aa2d590076279c4300e88dc0f9fad1972eabf4a9b327eed3f76723bdd5c" + // Y
    "5f2008" +
    "4445475858890225" + // CHR
    "7f4c1306082a8214004c0481185307ffffffffffffff" + // CHA
    "5f2506020501020100" + // CED 2025-12-10
    "5f2406030501020009" + // CXD 2035-12-09
    "5f3740" +
    "76d7833ff0a1366583992c693d80018dc2685f170958b220676b32c9b09b5251" + // r
    "4406b0a6864be810e1b5711d13c82aaa5d7c21290a5a8b1ac389f907d99f784c"   // s
```

## Reproduction recipe

```sh
# Pull the listing
curl -fsS "https://cvc.egk-tsp.de.atos.net/api/service/cvc" -o cvc-listing.json

# Filter to "Aktiv" roots and fetch the *_1 (CVC DER) file:
INT_ID=1776410492215   # current production
curl -fsS "https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/${INT_ID}/${INT_ID}_1" \
    -o DEZGW870226.cvc
shasum -a 256 DEZGW870226.cvc
# expect: 364d3a85fad1dbca9c4d8e1a2b9d07c61e4343abafe79dae38563887cf604a98

INT_ID=1766027554972   # current test (RU/TU)
curl -fsS "https://cvc.egk-tsp.de.atos.net/api/service/cvc/download/${INT_ID}/${INT_ID}_1" \
    -o DEGXX890225.cvc
shasum -a 256 DEGXX890225.cvc
# expect: 417d4bbfcf58a33ecce65fb513d7a2c0735106baa0e91a269865edd275db40f0
```

`openssl asn1parse -inform DER -in <file>` confirms `appl[33]` outer
wrapper containing `appl[78]` body, with `ecdsa-with-SHA256` OID inside
`appl[73]` (the SPKI), `1.2.276.0.76.4.152` inside `appl[76]` (the
role-OID), and `appl[55]` signature trailing.

The cross / link certs (every "Cross Zertifikat" row in the JSON
listing) have the same structure but with CAR ≠ CHR; they will be
needed once we walk a CV-cert chain from an end-entity SMC-B / HBA up
to whichever trust-anchor generation the relying card holds.

## Where the spec PDFs sit in this picture

- `gemSpec_CVC_Root` v1.9.1 — process spec: what the operator must do
  (issuance, publication, key rollover, cross-certs). Does **NOT**
  embed any key bytes.
- `gemSpec_CVC_TSP` — operational requirements for downstream Sub-CAs
  (D-Trust, T-Systems, Arvato, BITMARCK, Atos, gematik internal, Kubus IT)
  that get CVC-Sub-CA certs issued by the CVC-Root.
- `gemSpec_PKI` (current v2.26.0) — overall PKI policy. CV-cert PKI is
  one of two roots, distinct from the X.509 TSL roots already covered
  by `internal/c2c/keys/roots.go`.
- `gemSpec_Krypt` v2.29.0 — allowable algorithms; mandates brainpool*
  curves for ECC keys on G2 cards. Does NOT embed root key bytes.
- `gemProdT_CVC_Root_ECC` — Produkttypsteckbrief approving the operator
  (currently Eviden Germany GmbH v1.3.2, dated 2023-06-29). Names the
  operator; the operator's portal then publishes the keys.

So the chain is: **gematik spec → operator approval → operator's web
portal carries the bytes**. There is no gematik-signed XML feed
analogous to the X.509 TSL for the CV-cert PKI; the prior research's
"PDF-locked, no URL feed" assumption was *partially* wrong — there IS
a public URL feed, but it is the operator's own portal, not a gematik
host, and there is no detached gematik signature over the data.

## What I could NOT independently corroborate

1. **No second-source cross-check.** The byte values above come from
   one operator's web portal. I did not find them re-published in any
   gematik-hosted location, in a BSI hosted PKI directory, or in any
   gematik GitHub repository. The closest gematik-side primary source
   is `gematik/lib-smartcards` (`de.gematik.smartcards.g2icc.cvc.TrustCenter`
   class) which expects the operator to place the anchor DER files
   under `~/.config/.../input/trust-anchor/` but does not ship them in
   the repo; the test suite uses a developer-local trust store.
2. **Atos portal FINGERPRINT semantic.** The "FINGERPRINT" column in
   the JSON listing is *not* SHA-256 over the DER, body, point, SPKI,
   PEM or CHR that I tested. Best guess is a hash over the PDF
   visual-rendering or over a vendor-specific Atos serialization. For
   production trust pinning use **SHA-256 over the 220-byte DER**
   (`sha256(cert)` rows above), which I computed locally.
3. **gemSpec_PKI annex listing of the keys.** I did not download the
   current `gemSpec_PKI` v2.26.0 PDF to verify whether it embeds the
   public-key fingerprints in an annex. The spec landing page lists
   the chapters but the PDF gating could not be exhaustively probed
   here. If it does, those should be added as a second-source
   cross-check before any `internal/c2c/keys/cvc_roots.go` commit.
4. **No detached signature.** I did not find a gematik-signed
   manifest, OCSP, or TSL XML covering these CV-certs. Their integrity
   depends on operator custody + TLS.

## Recommended next steps before embedding in Go

1. Fetch `gemSpec_PKI` v2.26.0 PDF and check whether annex A lists the
   CVC-Root fingerprints — if so, store the gematik-side hash and
   compare to the operator-side bytes above; if not, document that the
   operator portal is the *only* public source.
2. Verify the active production root's self-signature with our own
   `internal/c2c/brainpool` ECDSA-verify against `(X, Y)` and the body
   bytes — that's a sanity check that the bytes we embed are
   self-consistent under our own crypto stack. The decoded body sha-256
   above is the message digest input.
3. Pull at least one Sub-CA CVC (e.g. `GEMATIK-CV-CA15` from the test
   PKI, which would have issued the slot-2 SMC-B's CV-certs) from the
   appropriate Sub-CA TSP and verify the chain `Sub-CA CVC → root
   DEGXX880224 or DEGXX890225` against the keys above. That's the
   integration test that makes the trust-anchor "real".
4. Repeat the fetch in 12 months and diff. The 8th-gen RU/TU root went
   active 2025-12-10; the 7th gen will become useless once cards stop
   accepting it. Production rolled from DEZGW860224 to DEZGW870226 on
   2026-04-15 — that's recent enough that some deployed devices may
   still hold only the 6th gen and rely on the cross-cert pair to
   bootstrap to gen 7.
