package model

import "time"

const (
	SYN_TIMEOUT        = 500 * time.Millisecond // max wait for SYN-ACK
	SEGMENT_TIMEOUT    = 700 * time.Millisecond // max wait for DATA-ACK
	MAX_RETRANSMISIONS = 3                      // how many times to retransmit a segment
	READ_DEADLINE      = 5 * time.Second        // read deadline for receiving packets
	SEGMENT_SIZE       = 1024                   // bytes per PSH segment
)
