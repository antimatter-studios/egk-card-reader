package main

import (
	"strings"
	"testing"
)

func TestOutputForPath(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"data.gdt", "gdt", false},
		{"DATA.GDT", "gdt", false},
		{"data.hl7", "hl7-adt", false},
		{"data.fhir.json", "hl7-fhir", false},
		{"data.json", "json", false},
		{"/abs/path/x.gdt", "gdt", false},
		{"data.txt", "", true},
		{"data", "", true},
	}
	for _, c := range cases {
		got, err := outputForPath(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("outputForPath(%q): err=%v, wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if got != c.want {
			t.Errorf("outputForPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEncoderKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hl7-fhir", "fhir"},
		{"hl7-adt", "hl7adt"},
		{"gdt", "gdt"},
		{"json", "json"},
		{"form", "form"},
	}
	for _, c := range cases {
		if got := encoderKey(c.in); got != c.want {
			t.Errorf("encoderKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReadCmdValidateDefaultsCardreader(t *testing.T) {
	cmd := &ReadCmd{Input: "cardreader"}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cmd.Output != "form" {
		t.Errorf("Output default = %q, want form", cmd.Output)
	}
}

func TestReadCmdValidateDefaultsFromPath(t *testing.T) {
	cmd := &ReadCmd{Input: "data.fhir.json"}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cmd.Output != "hl7-fhir" {
		t.Errorf("Output default = %q, want hl7-fhir", cmd.Output)
	}
}

func TestReadCmdValidatePathDefaultFails(t *testing.T) {
	cmd := &ReadCmd{Input: "data.txt"}
	err := cmd.Validate()
	if err == nil {
		t.Fatal("expected error from unrecognised extension")
	}
	if !strings.Contains(err.Error(), "cannot infer") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestReadCmdValidateOutputAllowed(t *testing.T) {
	for _, out := range []string{"form", "gdt", "hl7-fhir", "hl7-adt", "json"} {
		cmd := &ReadCmd{Input: "cardreader", Output: out}
		if err := cmd.Validate(); err != nil {
			t.Errorf("%s: %v", out, err)
		}
	}
}

func TestReadCmdValidateOutputRejected(t *testing.T) {
	cmd := &ReadCmd{Input: "cardreader", Output: "weird"}
	if err := cmd.Validate(); err == nil {
		t.Error("expected error for unsupported output")
	}
}

func TestReadCmdValidateFileFormRejected(t *testing.T) {
	cmd := &ReadCmd{Input: "cardreader", Output: "form", File: true}
	err := cmd.Validate()
	if err == nil {
		t.Error("expected error: form has no byte form")
	}
}

func TestReadCmdValidateDebugRequiresCardreader(t *testing.T) {
	cmd := &ReadCmd{Input: "data.gdt", Debug: true}
	if err := cmd.Validate(); err == nil {
		t.Error("expected error: --debug requires cardreader input")
	}
}

func TestReadCmdValidateDebugWithCardreader(t *testing.T) {
	cmd := &ReadCmd{Input: "cardreader", Debug: true}
	if err := cmd.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
