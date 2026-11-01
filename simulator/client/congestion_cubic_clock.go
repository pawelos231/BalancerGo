package client

import "time"

// cubicClock removes time during which local demand cannot use spare cwnd.
// Waiting for ACKs with a full congestion window is ordinary network time, not a pause.
type cubicClock struct {
	limited     bool
	pausedAt    time.Duration
	pausedUntil time.Duration // Zero means no known end, e.g. rwnd has closed.
	totalPaused time.Duration
}

// onSenderState observes real sender constraints without inspecting queues on remote hosts.
// A known 10 ms workload gap ends at readyAt even if the next observation arrives later.
func (policy *cubicPolicy) onSenderState(sender congestionSenderState) {
	clock := &policy.clock
	if clock.limited {
		end := sender.at
		if clock.pausedUntil != 0 {
			end = min(end, clock.pausedUntil)
		}
		clock.totalPaused += max(end-clock.pausedAt, 0)
	}

	hasSpareWindow := sender.flightBytes < policy.congestionWindowBytes
	noData := !sender.hasData
	receiverLimited := sender.receiveWindowBytes < policy.congestionWindowBytes
	waitingForGap := sender.at < sender.readyAt
	clock.limited = hasSpareWindow && (noData || receiverLimited || waitingForGap)
	clock.pausedAt = sender.at
	clock.pausedUntil = 0
	if clock.limited && waitingForGap && !noData && !receiverLimited {
		clock.pausedUntil = sender.readyAt
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
