package model

const DEFAULT_PACKET_SIZE = 1024

type Protocol string

const (
	TCP Protocol = "TCP"
	UDP Protocol = "UDP" // not used yet
)

type Packet struct {
	Key  ClientKey
	Flag Flag
	Size int    // Payload bytes, excluding headers and SYN/FIN sequence positions.
	Seq  uint64 // Logical byte position.
	Ack  uint64 // Next byte position expected from the peer.

	// How many bytes this endpoint is willing to receive starting at Ack.
	ReceiveWindowBytes uint16

	// SYN offers SACK; later ACKs can report received ranges beyond a gap.
	SACKPermitted bool
	SACKBlocks    []SACKBlock
}

// SequenceEnd is the next position after this packet's payload.
func (p Packet) SequenceEnd() (uint64, bool) {
	if p.Size < 0 {
		return 0, false
	}

	// How much to add to Seq to reach the end of this packet's range.
	length := uint64(p.Size)

	isSyn := p.Flag&FlagSYN != 0
	if isSyn {
		length++
	}

	isFin := p.Flag&FlagFIN != 0
	if isFin {
		length++
	}

	maxSequence := ^uint64(0)
	remainingRange := maxSequence - p.Seq
	if length > remainingRange {
		return 0, false
	}

	return p.Seq + length, true
}

// WireSize includes IPv4, TCP and its options, padded to a four-byte boundary.
func (p Packet) WireSize() int {
	optionBytes := 0
	if p.SACKPermitted {
		optionBytes += 2
	}
	if len(p.SACKBlocks) > 0 {
		// Logical uint64 positions still model the eight-byte TCP SACK block.
		optionBytes += 2 + 8*min(len(p.SACKBlocks), MaxSACKBlocks)
	}
	optionBytes = (optionBytes + 3) / 4 * 4
	return p.Size + 40 + optionBytes
}
