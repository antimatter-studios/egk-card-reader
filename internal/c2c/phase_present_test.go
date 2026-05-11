package c2c

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/christhomas/card-reader/internal/c2c/cvcert"
	"github.com/christhomas/card-reader/internal/c2c/keys"
)

// makeFakeCert builds a minimal *cvcert.Cert populated only with the fields
// phase 3 reads: CAR (issuer reference), Body (signed bytes) and Signature.
// The body/signature payloads are arbitrary — phase 3 doesn't crypto-verify
// them, only ships them to the eGK over APDU.
func makeFakeCert(carBytes, bodyMarker, sigMarker []byte) *cvcert.Cert {
	// Build a synthetic 7F4E TLV containing bodyMarker — phase 3 ships
	// c.Body verbatim, so any non-empty []byte works for the assertion.
	body := append([]byte{0x7F, 0x4E, byte(len(bodyMarker))}, bodyMarker...)
	return &cvcert.Cert{
		CAR:       string(carBytes),
		Body:      body,
		Signature: append([]byte(nil), sigMarker...),
	}
}

// scriptPair returns the two scriptEntries (MSE SET DST 22 81 B6, PSO
// VERIFY CERTIFICATE 2A 00 BE) that respond with the given SWs. Each
// entry matches on the first 4 bytes of the APDU so the test ignores the
// per-call Lc/data.
func scriptPair(swMSE, swPSO uint16) []scriptEntry {
	return []scriptEntry{
		{match: []byte{0x00, 0x22, 0x81, 0xB6}, resp: []byte{byte(swMSE >> 8), byte(swMSE & 0xFF)}},
		{match: []byte{0x00, 0x2A, 0x00, 0xBE}, resp: []byte{byte(swPSO >> 8), byte(swPSO & 0xFF)}},
	}
}

func TestPresentToVerifier_TwoCertChain_Success(t *testing.T) {
	// chain[0] = leaf (SMC-B AUT), chain[1] = root-signed CA. We present
	// in reverse: CA first, then leaf.
	leafCAR := []byte("CA-CHR-X")  // 8 bytes
	rootCAR := []byte("RT-CHR-Y")  // 8 bytes
	leaf := makeFakeCert(leafCAR, []byte{0xAA, 0xBB}, []byte{0xCC, 0xDD})
	leaf.CHR = "LF-CHR-Z"
	ca := makeFakeCert(rootCAR, []byte{0xEE, 0xFF}, []byte{0x11, 0x22})
	ca.CHR = "CA-CHR-X"

	card := &fakeCard{scripts: scriptPair(0x9000, 0x9000)}
	h := &Handshake{
		opts: Options{EGK: card},
		smcbChain: []DiscoveredCert{
			{Cert: leaf},
			{Cert: ca},
		},
		matchedRoot: &keys.Root{Name: "test-root"},
	}

	if err := h.phasePresentToVerifier(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 4 APDUs: MSE(CA-issuer=rootCAR) PSO(ca body) MSE(leaf-issuer=leafCAR) PSO(leaf body).
	if len(card.calls) != 4 {
		t.Fatalf("want 4 APDUs, got %d: %x", len(card.calls), card.calls)
	}

	// 1st call: MSE SET DST with CA's CAR (== rootCAR).
	assertMSESetDST(t, card.calls[0], rootCAR, "step 1 (root → CA)")
	// 2nd call: PSO VERIFY CERTIFICATE with CA's body+sig.
	assertPSOPayload(t, card.calls[1], ca, "step 1 (root → CA)")
	// 3rd call: MSE SET DST with leaf's CAR (== CA's CHR).
	assertMSESetDST(t, card.calls[2], leafCAR, "step 2 (CA → leaf)")
	// 4th call: PSO VERIFY CERTIFICATE with leaf's body+sig.
	assertPSOPayload(t, card.calls[3], leaf, "step 2 (CA → leaf)")
}

func TestPresentToVerifier_ThreeCertChain_Success(t *testing.T) {
	// leaf <- intermediate CA <- subroot CA (signed by trusted root).
	leafCAR := []byte("CAxCHRx1")
	caCAR := []byte("SBxCHRx2")
	subRtCAR := []byte("RTxCHRx3")
	leaf := makeFakeCert(leafCAR, []byte{0x01}, []byte{0x10})
	mid := makeFakeCert(caCAR, []byte{0x02}, []byte{0x20})
	sub := makeFakeCert(subRtCAR, []byte{0x03}, []byte{0x30})

	card := &fakeCard{scripts: scriptPair(0x9000, 0x9000)}
	h := &Handshake{
		opts: Options{EGK: card},
		smcbChain: []DiscoveredCert{
			{Cert: leaf},
			{Cert: mid},
			{Cert: sub},
		},
		matchedRoot: &keys.Root{Name: "test-root"},
	}

	if err := h.phasePresentToVerifier(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect 6 APDUs (2 per cert × 3 certs).
	if len(card.calls) != 6 {
		t.Fatalf("want 6 APDUs, got %d", len(card.calls))
	}

	// Present order: sub, mid, leaf.
	assertMSESetDST(t, card.calls[0], subRtCAR, "step 1")
	assertPSOPayload(t, card.calls[1], sub, "step 1")
	assertMSESetDST(t, card.calls[2], caCAR, "step 2")
	assertPSOPayload(t, card.calls[3], mid, "step 2")
	assertMSESetDST(t, card.calls[4], leafCAR, "step 3")
	assertPSOPayload(t, card.calls[5], leaf, "step 3")
}

func TestPresentToVerifier_PSORejectedWith6300(t *testing.T) {
	leaf := makeFakeCert([]byte("CARCAR-A"), []byte{0xAA}, []byte{0xBB})
	card := &fakeCard{scripts: []scriptEntry{
		{match: []byte{0x00, 0x22, 0x81, 0xB6}, resp: []byte{0x90, 0x00}},
		{match: []byte{0x00, 0x2A, 0x00, 0xBE}, resp: []byte{0x63, 0x00}},
	}}
	h := &Handshake{
		opts:        Options{EGK: card},
		smcbChain:   []DiscoveredCert{{Cert: leaf}},
		matchedRoot: &keys.Root{Name: "r"},
	}
	err := h.phasePresentToVerifier()
	if err == nil {
		t.Fatal("expected SW=6300 to surface as error")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T", err)
	}
	if ce.Phase != PhasePresentToVerifier {
		t.Errorf("Phase = %v, want PhasePresentToVerifier", ce.Phase)
	}
	if ce.Role != RoleEGK {
		t.Errorf("Role = %v, want RoleEGK", ce.Role)
	}
	if !strings.Contains(strings.ToLower(ce.Msg), "rejected") {
		t.Errorf("Msg = %q, want it to contain 'rejected'", ce.Msg)
	}
	if !strings.Contains(ce.Msg, "PSO VERIFY CERTIFICATE") {
		t.Errorf("Msg = %q, want it to identify PSO VERIFY CERTIFICATE", ce.Msg)
	}
}

func TestPresentToVerifier_PSORejectedWith6982(t *testing.T) {
	leaf := makeFakeCert([]byte("CARCAR-B"), []byte{0xAA}, []byte{0xBB})
	card := &fakeCard{scripts: []scriptEntry{
		{match: []byte{0x00, 0x22, 0x81, 0xB6}, resp: []byte{0x90, 0x00}},
		{match: []byte{0x00, 0x2A, 0x00, 0xBE}, resp: []byte{0x69, 0x82}},
	}}
	h := &Handshake{
		opts:        Options{EGK: card},
		smcbChain:   []DiscoveredCert{{Cert: leaf}},
		matchedRoot: &keys.Root{Name: "r"},
	}
	err := h.phasePresentToVerifier()
	if err == nil {
		t.Fatal("expected SW=6982 to surface as error")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T", err)
	}
	if ce.Phase != PhasePresentToVerifier || ce.Role != RoleEGK {
		t.Errorf("Phase=%v Role=%v", ce.Phase, ce.Role)
	}
	if !strings.Contains(ce.Msg, "6982") {
		t.Errorf("Msg = %q, want it to mention 6982", ce.Msg)
	}
	if !strings.Contains(strings.ToLower(ce.Msg), "security status") {
		t.Errorf("Msg = %q, want it to mention 'security status'", ce.Msg)
	}
}

func TestPresentToVerifier_MSERejectedSurfaces(t *testing.T) {
	// First APDU (MSE SET DST) rejected — we should bail before PSO.
	leaf := makeFakeCert([]byte("CARCAR-C"), []byte{0xAA}, []byte{0xBB})
	card := &fakeCard{scripts: []scriptEntry{
		{match: []byte{0x00, 0x22, 0x81, 0xB6}, resp: []byte{0x63, 0x00}},
	}}
	h := &Handshake{
		opts:        Options{EGK: card},
		smcbChain:   []DiscoveredCert{{Cert: leaf}},
		matchedRoot: &keys.Root{Name: "r"},
	}
	err := h.phasePresentToVerifier()
	if err == nil {
		t.Fatal("expected MSE rejection to surface")
	}
	if len(card.calls) != 1 {
		t.Errorf("expected exactly 1 APDU (MSE), got %d", len(card.calls))
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T", err)
	}
	if !strings.Contains(ce.Msg, "MSE SET DST") {
		t.Errorf("Msg = %q, want it to identify MSE SET DST", ce.Msg)
	}
}

func TestPresentToVerifier_EmptyChain(t *testing.T) {
	card := &fakeCard{}
	h := &Handshake{
		opts:        Options{EGK: card},
		matchedRoot: &keys.Root{Name: "r"},
	}
	err := h.phasePresentToVerifier()
	if err == nil {
		t.Fatal("expected precondition error")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T", err)
	}
	if ce.Phase != PhasePresentToVerifier {
		t.Errorf("Phase = %v, want PhasePresentToVerifier", ce.Phase)
	}
	if !strings.Contains(ce.Msg, "chain is empty") {
		t.Errorf("Msg = %q, want it to mention empty chain", ce.Msg)
	}
	if len(card.calls) != 0 {
		t.Errorf("expected no APDUs on precondition fail, got %d", len(card.calls))
	}
}

func TestPresentToVerifier_NoMatchedRoot(t *testing.T) {
	leaf := makeFakeCert([]byte("CARCAR-D"), []byte{0xAA}, []byte{0xBB})
	card := &fakeCard{}
	h := &Handshake{
		opts:      Options{EGK: card},
		smcbChain: []DiscoveredCert{{Cert: leaf}},
		// matchedRoot intentionally nil.
	}
	err := h.phasePresentToVerifier()
	if err == nil {
		t.Fatal("expected precondition error")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *c2c.Error, got %T", err)
	}
	if !strings.Contains(ce.Msg, "no trusted CVC-Root matched") {
		t.Errorf("Msg = %q, want it to mention missing matched root", ce.Msg)
	}
	if len(card.calls) != 0 {
		t.Errorf("expected no APDUs on precondition fail, got %d", len(card.calls))
	}
}

func TestPresentToVerifier_LongSignatureUsesLongLength(t *testing.T) {
	// A 256-byte signature (RSA-2048-shaped) forces the 5F37 length
	// encoding to 82 LL LL. Confirm our buildPSOPayload and the resulting
	// APDU still go out correctly.
	longSig := bytes.Repeat([]byte{0xC3}, 256)
	leaf := makeFakeCert([]byte("CARCAR-E"), []byte{0xAA, 0xBB}, longSig)
	card := &fakeCard{scripts: scriptPair(0x9000, 0x9000)}
	h := &Handshake{
		opts:        Options{EGK: card},
		smcbChain:   []DiscoveredCert{{Cert: leaf}},
		matchedRoot: &keys.Root{Name: "r"},
	}
	if err := h.phasePresentToVerifier(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(card.calls) != 2 {
		t.Fatalf("want 2 APDUs, got %d", len(card.calls))
	}
	pso := card.calls[1]
	// PSO APDU: 00 2A 00 BE 00 LLhi LLlo <payload> (extended length).
	if pso[0] != 0x00 || pso[1] != 0x2A || pso[2] != 0x00 || pso[3] != 0xBE {
		t.Errorf("PSO header bytes = %x, want 00 2A 00 BE", pso[:4])
	}
	if pso[4] != 0x00 {
		t.Errorf("PSO Lc form byte = %02X, want 00 (extended)", pso[4])
	}
	// 5F37 long-length signature TLV (sig length 256 = 0x0100 → 5F 37 82 01 00).
	if !bytes.Contains(pso, []byte{0x5F, 0x37, 0x82, 0x01, 0x00}) {
		t.Errorf("PSO payload missing 5F37 82 0100 long-length signature TLV")
	}
}

// --- helpers ------------------------------------------------------------

func assertMSESetDST(t *testing.T, apdu, wantCAR []byte, label string) {
	t.Helper()
	// Expected APDU: 00 22 81 B6 LL  83 LLcar <car>
	if len(apdu) < 5 {
		t.Errorf("%s: MSE APDU too short: %x", label, apdu)
		return
	}
	if !bytes.Equal(apdu[:4], []byte{0x00, 0x22, 0x81, 0xB6}) {
		t.Errorf("%s: MSE header = %x, want 00 22 81 B6", label, apdu[:4])
		return
	}
	lc := int(apdu[4])
	if 5+lc != len(apdu) {
		t.Errorf("%s: MSE Lc=%d but APDU has %d body bytes", label, lc, len(apdu)-5)
		return
	}
	body := apdu[5:]
	if len(body) < 2 || body[0] != 0x83 {
		t.Errorf("%s: MSE body tag = %02X, want 83", label, body[0])
		return
	}
	carLen := int(body[1])
	if 2+carLen != len(body) {
		t.Errorf("%s: tag-83 length %d does not match body remainder %d", label, carLen, len(body)-2)
		return
	}
	if !bytes.Equal(body[2:], wantCAR) {
		t.Errorf("%s: MSE CAR = %x, want %x", label, body[2:], wantCAR)
	}
}

func assertPSOPayload(t *testing.T, apdu []byte, cert *cvcert.Cert, label string) {
	t.Helper()
	if len(apdu) < 5 {
		t.Errorf("%s: PSO APDU too short: %x", label, apdu)
		return
	}
	if !bytes.Equal(apdu[:4], []byte{0x00, 0x2A, 0x00, 0xBE}) {
		t.Errorf("%s: PSO header = %x, want 00 2A 00 BE", label, apdu[:4])
		return
	}
	var payload []byte
	if apdu[4] == 0x00 && len(apdu) >= 7 {
		// extended length form
		payload = apdu[7:]
	} else {
		payload = apdu[5:]
	}
	// payload must begin with cert.Body (which already includes its 7F4E header)
	if !bytes.HasPrefix(payload, cert.Body) {
		t.Errorf("%s: PSO payload prefix mismatch.\n got: %x\nwant prefix: %x", label, payload, cert.Body)
		return
	}
	sigTLV := payload[len(cert.Body):]
	if len(sigTLV) < 3 || sigTLV[0] != 0x5F || sigTLV[1] != 0x37 {
		t.Errorf("%s: PSO trailing TLV tag = %x, want 5F37", label, sigTLV[:min2(3, len(sigTLV))])
		return
	}
	// Skip BER length and compare value.
	var sigVal []byte
	switch sigTLV[2] {
	case 0x81:
		if len(sigTLV) < 4 {
			t.Errorf("%s: truncated 5F37 81 length", label)
			return
		}
		sigVal = sigTLV[4:]
	case 0x82:
		if len(sigTLV) < 5 {
			t.Errorf("%s: truncated 5F37 82 length", label)
			return
		}
		sigVal = sigTLV[5:]
	default:
		if sigTLV[2] >= 0x80 {
			t.Errorf("%s: unexpected 5F37 length byte %02X", label, sigTLV[2])
			return
		}
		sigVal = sigTLV[3:]
	}
	if !bytes.Equal(sigVal, cert.Signature) {
		t.Errorf("%s: 5F37 signature value mismatch\n got: %x\nwant: %x", label, sigVal, cert.Signature)
	}
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
