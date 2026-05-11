package orga

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
)

// fakeSerialIO is a scripted ReadWriteCloser for T=1 transport tests. Each
// scripted exchange pairs an expected outbound Write with a canned inbound
// reply that is enqueued for subsequent Reads. Read/Write errors can be
// forced. Everything is recorded for assertions.
//
// Usage:
//
//	fake := newFakeSerialIO(t,
//	    exchange{wantWrite: buildBlock(...), reply: buildBlock(...)},
//	    exchange{wantWrite: nil /* accept any */, reply: buildBlock(...)},
//	)
//	term := &Terminal{io: fake, ns: map[byte]byte{}, timeout: time.Second}
//	... call methods ...
//	fake.assertDrained() // optional: all scripted writes consumed
type fakeSerialIO struct {
	mu sync.Mutex

	t       *testing.T
	scripts []exchange
	pos     int

	readBuf bytes.Buffer
	writes  [][]byte

	readErr  error
	writeErr error
	closed   bool

	// strictWrite, if true, fails the test when a Write doesn't match the
	// scripted exchange's wantWrite (when wantWrite != nil).
	strictWrite bool
}

type exchange struct {
	wantWrite []byte // expected outbound block. nil = accept anything.
	reply     []byte // bytes enqueued for Read after this Write.
}

func newFakeSerialIO(t *testing.T, scripts ...exchange) *fakeSerialIO {
	t.Helper()
	return &fakeSerialIO{t: t, scripts: scripts, strictWrite: true}
}

// preload enqueues bytes for Read without advancing the script. Useful when
// the function under test reads bytes before its first Write (e.g. an ATR).
func (f *fakeSerialIO) preload(b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readBuf.Write(b)
}

func (f *fakeSerialIO) setReadErr(err error)  { f.mu.Lock(); f.readErr = err; f.mu.Unlock() }
func (f *fakeSerialIO) setWriteErr(err error) { f.mu.Lock(); f.writeErr = err; f.mu.Unlock() }

// chunkSize, if non-zero, caps each Read return length so we exercise the
// chunked-read loop in readOneBlock. Default 0 = return all available.
func (f *fakeSerialIO) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		err := f.readErr
		f.readErr = nil // one-shot
		return 0, err
	}
	if f.readBuf.Len() == 0 {
		return 0, io.EOF
	}
	return f.readBuf.Read(p)
}

func (f *fakeSerialIO) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		err := f.writeErr
		f.writeErr = nil
		return 0, err
	}
	cp := append([]byte(nil), p...)
	f.writes = append(f.writes, cp)

	if f.pos >= len(f.scripts) {
		// No more scripted exchanges. Accept the write; caller likely doesn't
		// expect a reply. If a Read follows it'll EOF.
		return len(p), nil
	}
	want := f.scripts[f.pos].wantWrite
	if f.strictWrite && want != nil && !bytes.Equal(want, cp) {
		f.t.Errorf("fakeSerialIO write #%d:\n want %X\n got  %X", f.pos, want, cp)
	}
	if reply := f.scripts[f.pos].reply; len(reply) > 0 {
		f.readBuf.Write(reply)
	}
	f.pos++
	return len(p), nil
}

func (f *fakeSerialIO) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// assertDrained fails the test if any scripted exchanges remain unused.
func (f *fakeSerialIO) assertDrained() {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pos != len(f.scripts) {
		f.t.Errorf("fakeSerialIO: %d/%d exchanges consumed", f.pos, len(f.scripts))
	}
}

// errCloser is a serialIO whose Close returns a specific error. Used to
// verify Terminal.Close propagates the underlying io.Close error.
type errCloser struct {
	io.ReadWriter
	closeErr error
}

func (e *errCloser) Close() error { return e.closeErr }

// chunkedReader wraps a fakeSerialIO and caps each Read return length to n.
// Used to verify readOneBlock correctly accumulates byte fragments.
type chunkedReader struct {
	inner *fakeSerialIO
	cap   int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	n := len(p)
	if c.cap > 0 && n > c.cap {
		n = c.cap
	}
	return c.inner.Read(p[:n])
}
func (c *chunkedReader) Write(p []byte) (int, error) { return c.inner.Write(p) }
func (c *chunkedReader) Close() error                { return c.inner.Close() }

// errAfterReader returns n successful reads then an error.
type errAfterReader struct {
	reads [][]byte
	err   error
	idx   int
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.reads) {
		return 0, r.err
	}
	n := copy(p, r.reads[r.idx])
	r.idx++
	return n, nil
}

var errSentinel = errors.New("sentinel-io-error")
