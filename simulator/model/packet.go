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

// WireSize models IPv4 plus TCP headers without options
func (p Packet) WireSize() int {
	return p.Size + 40
}
