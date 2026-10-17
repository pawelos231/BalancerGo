package model

import "time"

const DEFAULT_PACKET_SIZE = 1024
const DEFAULT_PACKET_DELAY = 10 * time.Millisecond

type Protocol string

const (
	TCP Protocol = "TCP"
	UDP Protocol = "UDP"
)

type Packet struct {
	Key           ClientKey
	Flag          Flag
	Size          int
	Delay         time.Duration
	PlaceInStream uint32
}
