package tcp

import "time"

// PayloadSource supplies bytes and their workload deadlines without scheduling events.
// TCP pulls from it only when the connection's windows allow another new segment.
type PayloadSource interface {
	// Start prepares the first send when both source and connection are ready.
	Start(now time.Duration)

	// RemainingBytes excludes bytes already handed to TCP for transmission.
	RemainingBytes() int

	// NextSendAt returns the workload deadline and whether it fits in this run.
	NextSendAt() (time.Duration, bool)

	// Sent consumes new payload and advances the deadline from the actual send time.
	// Retransmissions never consume the workload a second time.
	Sent(payloadBytes int, now time.Duration)
}
