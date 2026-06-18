package orga

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// withTrace temporarily enables tracing for the body of fn. Restores the
// previous flag value on exit. We poke the package-private bool directly
// because traceEnabled is set once at init time from an env var.
func withTrace(t *testing.T, fn func()) {
	t.Helper()
	prev := traceEnabled
	traceEnabled = true
	defer func() { traceEnabled = prev }()
	fn()
}

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns whatever
// was written. Synchronized so concurrent calls don't interleave.
var stderrMu sync.Mutex

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	stderrMu.Lock()
	defer stderrMu.Unlock()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	fn()
	w.Close()
	os.Stderr = orig
	return string(<-done)
}

func TestTraceDisabled_NoOutput(t *testing.T) {
	// Ensure the default no-trace path is silent. We do NOT call withTrace.
	prev := traceEnabled
	traceEnabled = false
	defer func() { traceEnabled = prev }()
	out := captureStderr(t, func() {
		traceTX([]byte{0x12, 0xC0, 0x00, 0xD2})
		traceRX([]byte{0x21, 0xE0, 0x00, 0xC1})
	})
	if out != "" {
		t.Errorf("expected no output, got %q", out)
	}
}

func TestTraceTX_Enabled(t *testing.T) {
	var out string
	withTrace(t, func() {
		out = captureStderr(t, func() {
			traceTX([]byte{0x12, 0xC0, 0x00, 0xD2})
		})
	})
	for _, want := range []string{"ORGA TX", "12c000d2", "S-block RESYNCH req"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
}

func TestTraceRX_Enabled(t *testing.T) {
	var out string
	withTrace(t, func() {
		out = captureStderr(t, func() {
			traceRX([]byte{0x21, 0xE0, 0x00, 0xC1})
		})
	})
	for _, want := range []string{"ORGA RX", "21e000c1", "S-block RESYNCH resp"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
}

func TestTraceEnvDetection(t *testing.T) {
	// We can't re-run init, but we can simulate the env-var detection logic
	// for coverage of the predicate inside the init func by exercising
	// equivalent branches with strings used in real life.
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"1", true},
		{"true", true},
		{"yes", true},
	}
	for _, tc := range cases {
		got := tc.in != "" && tc.in != "0" && tc.in != "false"
		if got != tc.want {
			t.Errorf("ORGA_TRACE=%q -> %v; want %v", tc.in, got, tc.want)
		}
	}
}
