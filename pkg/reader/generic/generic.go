// Package generic drives any PC/SC-compatible card reader — Cherry ST-2100,
// OMNIKEY 3121, REINER cyberJack, Identiv uTrust, etc. It's the "generic"
// driver because PC/SC is the cross-vendor standard. Wraps github.com/ebfe/scard.
//
// A *Card implements pkg/reader.Card; combine with pkg/egk for an
// eGK read pipeline identical to the ORGA path.
package generic

import (
	"errors"
	"fmt"
	"time"

	"github.com/ebfe/scard"
)

// Reader is a PC/SC context with the list of attached readers it found.
// Close releases the context.
type Reader struct {
	ctx     *scard.Context
	readers []string
}

// Open establishes a PC/SC context and lists readers. Returns an error if
// pcscd / the macOS CCID stack isn't reachable or no readers are attached.
func Open() (*Reader, error) {
	ctx, err := scard.EstablishContext()
	if err != nil {
		return nil, fmt.Errorf("PC/SC: cannot establish context: %w", err)
	}
	rs, err := ctx.ListReaders()
	if err != nil {
		ctx.Release()
		return nil, fmt.Errorf("PC/SC: list readers: %w", err)
	}
	if len(rs) == 0 {
		ctx.Release()
		return nil, errors.New("PC/SC: no readers found — plug in your CCID reader and ensure pcscd is running")
	}
	return &Reader{ctx: ctx, readers: rs}, nil
}

// Readers returns the names of all attached PC/SC readers.
func (r *Reader) Readers() []string { return r.readers }

// Close releases the PC/SC context.
func (r *Reader) Close() error { return r.ctx.Release() }

// WaitForCard polls until any reader has a present, non-mute card or the
// timeout elapses. Returns the reader name that has the card.
func (r *Reader) WaitForCard(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		states := make([]scard.ReaderState, len(r.readers))
		for i, name := range r.readers {
			states[i].Reader = name
			states[i].CurrentState = scard.StateUnaware
		}
		err := r.ctx.GetStatusChange(states, 500*time.Millisecond)
		if err != nil && !errors.Is(err, scard.ErrTimeout) {
			return "", err
		}
		for i, s := range states {
			if s.EventState&scard.StatePresent != 0 && s.EventState&scard.StateMute == 0 {
				return r.readers[i], nil
			}
		}
	}
	return "", fmt.Errorf("PC/SC: no card present after %s", timeout)
}

// Present reports whether any attached reader currently holds a present,
// non-mute card, returning the reader name when one does. Unlike WaitForCard
// it does not loop: a single GetStatusChange poll with a zero timeout returns
// the current state immediately, so it is cheap to call on a watch interval.
func (r *Reader) Present() (name string, present bool, err error) {
	states := make([]scard.ReaderState, len(r.readers))
	for i, n := range r.readers {
		states[i].Reader = n
		states[i].CurrentState = scard.StateUnaware
	}
	if err := r.ctx.GetStatusChange(states, 0); err != nil && !errors.Is(err, scard.ErrTimeout) {
		return "", false, err
	}
	for i, s := range states {
		if s.EventState&scard.StatePresent != 0 && s.EventState&scard.StateMute == 0 {
			return r.readers[i], true, nil
		}
	}
	return "", false, nil
}

// Connect opens a session with the card currently in the named reader.
func (r *Reader) Connect(readerName string) (*Card, error) {
	c, err := r.ctx.Connect(readerName, scard.ShareShared, scard.ProtocolAny)
	if err != nil {
		return nil, fmt.Errorf("PC/SC: connect %s: %w", readerName, err)
	}
	return &Card{inner: c}, nil
}

// Card is one connected PC/SC card session. Satisfies pkg/reader.Card.
type Card struct {
	inner *scard.Card
}

// Transmit forwards a single APDU to the card and returns data+SW1SW2.
func (c *Card) Transmit(apdu []byte) ([]byte, error) {
	return c.inner.Transmit(apdu)
}

// ATR returns the answer-to-reset bytes as reported by the PC/SC stack.
func (c *Card) ATR() ([]byte, error) {
	st, err := c.inner.Status()
	if err != nil {
		return nil, err
	}
	return st.Atr, nil
}

// Close disconnects this card session, leaving the card powered in the reader.
func (c *Card) Close() error {
	return c.inner.Disconnect(scard.LeaveCard)
}
