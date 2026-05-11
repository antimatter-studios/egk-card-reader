package document

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// writeTempFile writes b to a freshly-created file under t.TempDir().
func writeTempFile(t *testing.T, b []byte, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// byteReader is a small adapter so tests can pass []byte to functions that
// expect io.Reader without importing bytes everywhere.
func byteReader(b []byte) io.Reader { return bytes.NewReader(b) }
