package model

import "time"

// DefaultReceiveBufferBytes fits the TCP window field without window scaling.
const DefaultReceiveBufferBytes = 65535

// Idle limits are separate from retransmission timers; zero disables idle cleanup.
const (
	DefaultClientIdleTimeout        = 20 * time.Second
	DefaultLBHalfOpenIdleTimeout    = 5 * time.Second
	DefaultLBEstablishedIdleTimeout = 50 * time.Second
)

const (
	SYN_TIMEOUT        = 1000 * time.Millisecond  // max wait for SYN-ACK
	SEGMENT_TIMEOUT    = 1000 * time.Millisecond  // max wait for DATA-ACK
	MAX_RETRANSMISIONS = 30                       // how many times to retransmit a segment
	READ_DEADLINE      = DefaultClientIdleTimeout // read deadline for receiving packets
	SEGMENT_SIZE       = 1024                     // bytes per PSH segment
)
