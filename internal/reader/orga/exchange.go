package orga

import (
	"fmt"
	"time"
)

// resync issues S(RESYNCH request) and consumes the matching response.
// Resets the T=1 sequence numbers on both sides.
func (t *Terminal) resync() error {
	hostToTerm := (terminalAddr << 4) | hostAddr
	rb := buildBlock(hostToTerm, 0xC0, nil)
	if _, err := t.io.Write(rb); err != nil {
		return err
	}
	blk, _, err := readOneBlock(t.io, time.Now().Add(t.timeout))
	if err != nil {
		return err
	}
	if blk.PCB != 0xE0 {
		return fmt.Errorf("expected S(RESYNCH response), got PCB=%02X", blk.PCB)
	}
	t.ns = map[byte]byte{}
	t.ifsNegotiated = false
	return nil
}

// transactWithNAD sends inf as an I-block to the peer addressed by dad,
// handles any in-flight S-block (IFS/WTX) and R-block (NAK) negotiations,
// reassembles chained I-block responses, and returns the final response INF
// (which is data+SW1SW2 for ICC peers).
func (t *Terminal) transactWithNAD(dad byte, inf []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	outNAD := (dad << 4) | hostAddr
	inNAD := swapNAD(outNAD)
	ns := t.ns[dad]
	pcb := byte(0) | (ns << 6)

	if err := t.writeBlock(outNAD, pcb, inf); err != nil {
		return nil, err
	}
	t.ns[dad] = ns ^ 1

	deadline := time.Now().Add(t.timeout)
	var accum []byte
	for {
		blk, _, err := readOneBlock(t.io, deadline)
		if err != nil {
			return nil, err
		}
		if blk.NAD != inNAD && blk.NAD != 0x00 {
			return nil, fmt.Errorf("orga: unexpected NAD %02X (expected %02X)", blk.NAD, inNAD)
		}

		switch {
		case isIBlock(blk.PCB):
			accum = append(accum, blk.INF...)
			if iBlockMore(blk.PCB) {
				// Send R-block to ack chained I, request next.
				rPCB := byte(0x80) | ((iBlockNS(blk.PCB) ^ 1) << 4)
				if err := t.writeBlock(outNAD, rPCB, nil); err != nil {
					return nil, err
				}
				continue
			}
			return accum, nil

		case isRBlock(blk.PCB):
			return nil, fmt.Errorf("orga: peer R-block err=%d (PCB=%02X)", blk.PCB&0x0F, blk.PCB)

		case isSBlock(blk.PCB):
			// Auto-ack S-block requests. Response bit (5) toggled in PCB.
			respPCB := blk.PCB | 0x20
			if blk.PCB&0x20 != 0 {
				return nil, fmt.Errorf("orga: unexpected S-block response PCB=%02X", blk.PCB)
			}
			if blk.PCB&0x1F == 0x01 {
				t.ifsNegotiated = true
			}
			if err := t.writeBlock(outNAD, respPCB, blk.INF); err != nil {
				return nil, err
			}
			// Loop continues — terminal will eventually send the I-block.
		}
	}
}

func (t *Terminal) writeBlock(nad, pcb byte, inf []byte) error {
	b := buildBlock(nad, pcb, inf)
	_, err := t.io.Write(b)
	return err
}
