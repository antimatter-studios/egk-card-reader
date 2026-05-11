package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/christhomas/card-reader/internal/document"
)

func TestFileWrite(t *testing.T) {
	dir := t.TempDir()
	doc := &document.Document{Format: "x", Extension: ".x", Bytes: []byte("hello")}
	w := File{Dir: dir, BaseName: "out"}
	if err := w.Write(doc); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.x"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("contents = %q", got)
	}
}

func TestFileWriteCreatesDir(t *testing.T) {
	// Dir does not exist yet — Write should mkdir.
	parent := t.TempDir()
	nested := filepath.Join(parent, "fresh", "subdir")
	w := File{Dir: nested, BaseName: "name"}
	if err := w.Write(&document.Document{Extension: ".bin", Bytes: []byte("x")}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nested, "name.bin")); err != nil {
		t.Errorf("file not present: %v", err)
	}
}

func TestFileWriteDefaultsToCwd(t *testing.T) {
	// Dir empty — defaults to ".". Use t.Chdir() (Go 1.24+) so cleanup is automatic.
	t.Chdir(t.TempDir())
	w := File{BaseName: "out"}
	if err := w.Write(&document.Document{Extension: ".x", Bytes: []byte("z")}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat("out.x"); err != nil {
		t.Errorf("file not present: %v", err)
	}
}

func TestFileWriteMkdirError(t *testing.T) {
	// Dir is a regular file → mkdir fails.
	parent := t.TempDir()
	regular := filepath.Join(parent, "file")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := File{Dir: filepath.Join(regular, "child"), BaseName: "x"}
	if err := w.Write(&document.Document{Extension: ".bin"}); err == nil {
		t.Error("expected mkdir error")
	}
}

func TestFileWriteWriteError(t *testing.T) {
	// Make the directory read-only so WriteFile fails. macOS/Linux behaviour.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	w := File{Dir: dir, BaseName: "x"}
	err := w.Write(&document.Document{Extension: ".bin", Bytes: []byte("y")})
	if err == nil {
		// Running as root would let the write through. Just skip in that case.
		t.Skip("read-only dir allowed write; running as root?")
	}
}

func TestStdoutWrite(t *testing.T) {
	// Redirect os.Stdout to a pipe, capture, restore.
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	if err := (Stdout{}).Write(&document.Document{Bytes: []byte("hello stdout")}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w.Close()
	os.Stdout = orig

	got := <-done
	if string(got) != "hello stdout" {
		t.Errorf("captured = %q", got)
	}
}

// captureWriter is an in-memory Writer used to test Multi.
type captureWriter struct {
	got []byte
	err error
}

func (c *captureWriter) Write(doc *document.Document) error {
	if c.err != nil {
		return c.err
	}
	c.got = append([]byte{}, doc.Bytes...)
	return nil
}

func TestMultiWrite(t *testing.T) {
	a := &captureWriter{}
	b := &captureWriter{}
	mw := Multi{Writers: []Writer{a, b}}
	if err := mw.Write(&document.Document{Bytes: []byte("payload")}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if string(a.got) != "payload" || string(b.got) != "payload" {
		t.Errorf("not all writers received: a=%q b=%q", a.got, b.got)
	}
}

func TestMultiWriteAbortsOnError(t *testing.T) {
	a := &captureWriter{}
	failing := &captureWriter{err: fmt.Errorf("boom")}
	c := &captureWriter{} // should NOT receive
	mw := Multi{Writers: []Writer{a, failing, c}}
	err := mw.Write(&document.Document{Bytes: []byte("x")})
	if err == nil {
		t.Error("expected error")
	}
	if string(a.got) != "x" {
		t.Error("first writer should have received")
	}
	if c.got != nil {
		t.Error("third writer should NOT have received after failing writer")
	}
}

func TestMultiWriteEmpty(t *testing.T) {
	if err := (Multi{}).Write(&document.Document{Bytes: []byte("x")}); err != nil {
		t.Errorf("empty Multi should succeed silently: %v", err)
	}
}
