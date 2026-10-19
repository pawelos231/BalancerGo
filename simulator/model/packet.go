package model

const DEFAULT_PACKET_SIZE = 1024

type Protocol string

const (
	TCP Protocol = "TCP"
	UDP Protocol = "UDP"
)

type Packet struct {
	Key           ClientKey
	Flag          Flag
	Size          int // payload bytes; excludes the modeled transport/network headers
	PlaceInStream uint32
}

// WireSize models IPv4 plus TCP headers without options. This stage does not
// implement UDP packets, link-layer headers, fragmentation, or TCP options.
func (p Packet) WireSize() int { return p.Size + 40 }
