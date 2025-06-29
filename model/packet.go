package model

import "time"

const DEFAULT_PACKET_SIZE = 1024                   // default packet size in bytes
const DEFAULT_PACKET_DELAY = 10 * time.Millisecond // default delay between packets
type Protocol string

type Packet struct {
	Key           ClientKey     // unique key of the packet based on source (client) and destination IP/port (server) it is a tuple of source IP, source port, destination IP, destination port, and protocol
	Flag          Flag          // TCP flags (SYN, FIN, RST)
	Size          int           // size of the packet in bytes
	Delay         time.Duration // delay before sending the packet (optional, can be used for simulating network conditions)
	PlaceInStream uint32        // position in the stream, used for ordering packets
}
