// Package output persists a Document to a destination — stdout, a file,
// (eventually) an HTTP endpoint or MLLP socket. It knows nothing about the
// document's contents beyond Bytes and Extension; the document package owns
// what the contents are.
package output

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/antimatter-studios/egk-card-reader/pkg/document"
)

// Writer persists a Document somewhere. Implementations cover stdout and
// file-on-disk today; new destinations slot in by implementing this interface.
type Writer interface {
	Write(doc *document.Document) error
}

// Stdout writes the document bytes to os.Stdout. Useful for piping into
// other tools (e.g. `card-reader --output-gdt | xdt-validate`).
type Stdout struct{}

func (Stdout) Write(doc *document.Document) error {
	_, err := os.Stdout.Write(doc.Bytes)
	return err
}

// File creates "Dir/BaseName+Extension" and writes the bytes there. It logs
// the resolved path to stderr so users who pipe stdout elsewhere can still
// see where the file landed.
type File struct {
	Dir      string
	BaseName string
}

func (fw File) Write(doc *document.Document) error {
	dir := fw.Dir
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, fw.BaseName+doc.Extension)
	if err := os.WriteFile(path, doc.Bytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s (%d bytes)\n", path, len(doc.Bytes))
	return nil
}

// Multi fans the document out to several writers in order. Useful when a
// deployment wants both an archived file copy and a piped feed.
type Multi struct {
	Writers []Writer
}

func (mw Multi) Write(doc *document.Document) error {
	for _, w := range mw.Writers {
		if err := w.Write(doc); err != nil {
			return err
		}
	}
	return nil
}
