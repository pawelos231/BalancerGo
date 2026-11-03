package client

import "time"

// finiteStream supplies one client's payload and pause between actual sends.
// TCP pulls this information when its receive and congestion windows allow sending.
type finiteStream struct {
	remaining  int
	gap        time.Duration
	endTime    time.Duration
	nextSendAt time.Duration
	withinRun  bool
}

// newFiniteStream keeps the requested byte count without creating packets or events.
func newFiniteStream(bytes int, gap, endTime time.Duration) *finiteStream {
	if bytes < 0 || gap < 0 {
		panic("client: stream size and gap must be nonnegative")
	}
	return &finiteStream{remaining: bytes, gap: gap, endTime: endTime}
}

// Start begins the pause once both the connection and payload are ready.
func (stream *finiteStream) Start(now time.Duration) {
	stream.withinRun = stream.gap <= stream.endTime-now
	if stream.withinRun {
		stream.nextSendAt = now + stream.gap
	}
}

func (stream *finiteStream) RemainingBytes() int {
	return stream.remaining
}

// NextSendAt returns the next eligible send time and whether it fits this run.
func (stream *finiteStream) NextSendAt() (time.Duration, bool) {
	return stream.nextSendAt, stream.withinRun
}

// Sent starts the next pause from the actual send, not an earlier missed deadline.
// With a 10 ms gap, a send delayed until 30 ms makes the next one ready at 40 ms.
func (stream *finiteStream) Sent(payloadBytes int, now time.Duration) {
	stream.remaining -= payloadBytes
	stream.Start(now)
}
