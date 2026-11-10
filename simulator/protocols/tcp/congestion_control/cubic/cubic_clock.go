package cubic

import (
	"time"

	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

// cubicClock removes time during which local demand cannot use spare cwnd.
// Waiting for ACKs with a full congestion window is ordinary network time, not a pause.
type cubicClock struct {
	limited     bool
	pausedAt    time.Duration
	pausedUntil time.Duration // Zero means no known end, e.g. rwnd has closed.
	totalPaused time.Duration
}

// OnSenderState observes real sender constraints without inspecting queues on remote hosts.
// A known 10 ms workload gap ends at readyAt even if the next observation arrives later.
func (policy *cubicPolicy) OnSenderState(sender congestioncontrol.SenderState) {
	clock := &policy.clock
	if clock.limited {
		end := sender.At
		if clock.pausedUntil != 0 {
			end = min(end, clock.pausedUntil)
		}
		clock.totalPaused += max(end-clock.pausedAt, 0)
	}

	hasSpareWindow := sender.FlightBytes < policy.CongestionWindowBytes
	noData := !sender.HasData
	receiverLimited := sender.ReceiveWindowBytes < policy.CongestionWindowBytes
	waitingForGap := sender.At < sender.ReadyAt
	clock.limited = hasSpareWindow && (noData || receiverLimited || waitingForGap)
	clock.pausedAt = sender.At
	clock.pausedUntil = 0
	if clock.limited && waitingForGap && !noData && !receiverLimited {
		clock.pausedUntil = sender.ReadyAt
	}
}

// activeAt is a virtual clock that stops during application/rwnd pauses.
func (clock *cubicClock) activeAt(now time.Duration) time.Duration {
	paused := clock.totalPaused
	if clock.limited {
		end := now
		if clock.pausedUntil != 0 {
			end = min(end, clock.pausedUntil)
		}
		paused += max(end-clock.pausedAt, 0)
	}
	return max(now-paused, 0)
}
