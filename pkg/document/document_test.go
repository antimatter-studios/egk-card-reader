package document

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/christhomas/card-reader/pkg/egk"
)

func TestCaptureBytesHappy(t *testing.T) {
	doc, err := captureBytes("x", ".x", func(w io.Writer) error {
		_, err := io.WriteString(w, "hello")
		return err
	})
	if err != nil {
		t.Fatalf("captureBytes: %v", err)
	}
	if doc.Format != "x" || doc.Extension != ".x" {
		t.Errorf("Document = %+v", doc)
	}
	if string(doc.Bytes) != "hello" {
		t.Errorf("Bytes = %q", doc.Bytes)
	}
}

func TestCaptureBytesError(t *testing.T) {
	_, err := captureBytes("x", ".x", func(io.Writer) error {
		return fmt.Errorf("boom")
	})
	if err == nil {
		t.Error("expected error")
	}
}

func TestBillingIK(t *testing.T) {
	if got := billingIK(egk.InsuranceData{BillingInsurerID: "A", InsurerID: "B"}); got != "A" {
		t.Errorf("billingIK with billing set = %q", got)
	}
	if got := billingIK(egk.InsuranceData{InsurerID: "B"}); got != "B" {
		t.Errorf("billingIK fallback = %q", got)
	}
	if got := billingIK(egk.InsuranceData{}); got != "" {
		t.Errorf("billingIK empty = %q", got)
	}
}

func TestBillingName(t *testing.T) {
	if got := billingName(egk.InsuranceData{BillingInsurerName: "A", InsurerName: "B"}); got != "A" {
		t.Errorf("billingName with billing set = %q", got)
	}
	if got := billingName(egk.InsuranceData{InsurerName: "B"}); got != "B" {
		t.Errorf("billingName fallback = %q", got)
	}
	if got := billingName(egk.InsuranceData{}); got != "" {
		t.Errorf("billingName empty = %q", got)
	}
}

func TestEncoderRegistry(t *testing.T) {
	want := map[string]struct {
		format, ext string
	}{
		"gdt":    {"gdt", ".gdt"},
		"fhir":   {"fhir", ".fhir.json"},
		"hl7adt": {"hl7adt", ".hl7"},
		"json":   {"json", ".json"},
	}
	for key, w := range want {
		enc, ok := Encoders[key]
		if !ok {
			t.Errorf("encoder %q missing", key)
			continue
		}
		if enc.Format() != w.format {
			t.Errorf("%s format = %q", key, enc.Format())
		}
		if enc.Extension() != w.ext {
			t.Errorf("%s extension = %q", key, enc.Extension())
		}
	}
}

func TestEncodeNilCardData(t *testing.T) {
	// All three wire encoders explicitly check for nil and return an error.
	// (formMappingJSON has no nil-check and will panic — see the separate
	// TestJSONEncoderPanicsOnNil test that asserts this current behaviour
	// so a future fix is visible.)
	for _, key := range []string{"gdt", "fhir", "hl7adt"} {
		enc := Encoders[key]
		_, err := enc.Encode(nil, nil)
		if err == nil {
			t.Errorf("%s should reject nil CardData", key)
		} else if !strings.Contains(err.Error(), "nil") {
			t.Errorf("%s error = %q", key, err.Error())
		}
	}
}

// TestJSONEncoderPanicsOnNil pins current behaviour: json doesn't check nil
// and the call panics inside FormMapping. If json grows an explicit nil-check
// later, update this test and TestEncodeNilCardData accordingly.
func TestJSONEncoderPanicsOnNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil CardData")
		}
	}()
	_, _ = Encoders["json"].Encode(nil, nil)
}
