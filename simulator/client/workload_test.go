package client

import (
	"testing"
	"time"
)

// The first packet is ready at 15 ms but TCP's window delays it until 30 ms.
// The next pause starts at that actual send, leaving the next packet ready at 40 ms.
func TestFiniteStreamGapStartsFromActualSend(t *testing.T) {
	stream := newFiniteStream(2048, 10*time.Millisecond, time.Second)
	stream.Start(5 * time.Millisecond)
	readyAt, withinRun := stream.NextSendAt()
	if readyAt != 15*time.Millisecond || !withinRun {
		t.Fatalf("first send readiness = %s, within run=%v", readyAt, withinRun)
	}

	stream.Sent(1024, 30*time.Millisecond)
	readyAt, withinRun = stream.NextSendAt()
	if readyAt != 40*time.Millisecond || !withinRun || stream.RemainingBytes() != 1024 {
		t.Fatalf("delayed send left deadline=%s, within run=%v, remaining=%d",
			readyAt, withinRun, stream.RemainingBytes())
	}
}

// A send exactly at the end time is allowed; one nanosecond later is outside the run.
func TestFiniteStreamIncludesEndTime(t *testing.T) {
	stream := newFiniteStream(2048, 10*time.Millisecond, 50*time.Millisecond)
	stream.Start(40 * time.Millisecond)
	readyAt, withinRun := stream.NextSendAt()
	if readyAt != 50*time.Millisecond || !withinRun {
		t.Fatalf("inclusive boundary = %s, within run=%v", readyAt, withinRun)
	}

	stream.Start(40*time.Millisecond + time.Nanosecond)
	_, withinRun = stream.NextSendAt()
	if withinRun {
		t.Fatal("a send beyond the end time was marked available")
	}
}

// Zero gap adds no waiting, even when the previous send happened at the end time.
func TestFiniteStreamZeroGapIsImmediatelyReady(t *testing.T) {
	stream := newFiniteStream(1024, 0, time.Second)
	stream.Start(0)
	stream.Sent(1024, time.Second)
	readyAt, withinRun := stream.NextSendAt()
	if readyAt != time.Second || !withinRun || stream.RemainingBytes() != 0 {
		t.Fatalf("zero-gap stream = deadline %s, within run=%v, remaining=%d",
			readyAt, withinRun, stream.RemainingBytes())
	}
}
