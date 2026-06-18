package orga

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// serialIO is the subset of *os.File used by Terminal — splitting it out lets
// tests substitute an in-memory ReadWriteCloser without touching termios.
type serialIO interface {
	io.ReadWriteCloser
}

// T=1 block primitives.

type block struct {
	NAD, PCB byte
	INF      []byte
}

func buildBlock(nad, pcb byte, inf []byte) []byte {
	hdr := []byte{nad, pcb, byte(len(inf))}
	full := make([]byte, 0, len(hdr)+len(inf)+1)
	full = append(full, hdr...)
	full = append(full, inf...)
	full = append(full, lrc(full))
	return full
}

func lrc(b []byte) byte {
	var x byte
	for _, c := range b {
		x ^= c
	}
	return x
}

// parseBlock decodes one T=1 block from buf. Returns (blk, totalConsumed, error).
// errIncomplete signals the caller to read more bytes and retry.
var errIncomplete = errors.New("orga: incomplete T=1 block")

func parseBlock(buf []byte) (block, int, error) {
	if len(buf) < 4 {
		return block{}, 0, errIncomplete
	}
	ln := int(buf[2])
	total := 3 + ln + 1
	if len(buf) < total {
		return block{}, 0, errIncomplete
	}
	blk := block{
		NAD: buf[0],
		PCB: buf[1],
		INF: append([]byte(nil), buf[3:3+ln]...),
	}
	want := lrc(buf[:3+ln])
	if want != buf[3+ln] {
		return blk, total, fmt.Errorf("orga: LRC mismatch want %02X got %02X (block %X)", want, buf[3+ln], buf[:total])
	}
	return blk, total, nil
}

// swapNAD returns the peer's-view NAD: source ↔ destination nibbles swapped.
func swapNAD(n byte) byte { return (n>>4)&0x0F | (n&0x0F)<<4 }

// PCB shape predicates.
func isIBlock(pcb byte) bool { return pcb&0x80 == 0 }
func isRBlock(pcb byte) bool { return pcb&0xC0 == 0x80 }
func isSBlock(pcb byte) bool { return pcb&0xC0 == 0xC0 }
func iBlockNS(pcb byte) byte { return (pcb >> 6) & 1 }
func iBlockMore(pcb byte) bool { return pcb&0x20 != 0 }

// readOneBlock reads bytes until a complete T=1 block is buffered, then
// returns it. Honors deadline.
func readOneBlock(r io.Reader, deadline time.Time) (block, []byte, error) {
	buf := make([]byte, 0, 264)
	tmp := make([]byte, 256)
	for {
		if time.Now().After(deadline) {
			return block{}, buf, fmt.Errorf("orga: read timeout (have %d bytes: %X)", len(buf), buf)
		}
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			blk, total, perr := parseBlock(buf)
			if perr == nil && total == len(buf) {
				return blk, buf, nil
			}
			if perr != nil && !errors.Is(perr, errIncomplete) {
				return blk, buf, perr
			}
		}
		if err != nil && err != io.EOF {
			return block{}, buf, err
		}
	}
}
