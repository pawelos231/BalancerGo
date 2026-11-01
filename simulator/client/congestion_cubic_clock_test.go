package client

import (
	"testing"
	"time"
)

// Inserting a short workload pause must leave the same cubic window at the same active time.
func TestCubicClockExcludesKnownGapAndReceiverPause(t *testing.T) {
	for _, reason := range []string{"workload gap", "receive window"} {
		t.Run(reason, func(t *testing.T) {
			baseline, paused := cubicTestPolicy(10), cubicTestPolicy(10)
			baseline.onACK(cubicTestACK(0, 1024))
			paused.onACK(cubicTestACK(0, 1024))
			sender := congestionSenderState{
				at: 100 * time.Millisecond, hasData: true, receiveWindowBytes: 65535,
			}
			if reason == "workload gap" {
				sender.readyAt = 200 * time.Millisecond
			} else {
				sender.receiveWindowBytes = 0
			}
			paused.onSenderState(sender)
			paused.onACK(cubicTestACK(150*time.Millisecond, 1024))
			if paused.windowSegments != baseline.windowSegments {
				t.Fatal("an ACK grew CUBIC while the sender was locally limited")
			}

			// The gap is known to end at 200 ms, even if only observed again at 300 ms.
			resumeAt := 300 * time.Millisecond
			activeAt := 200 * time.Millisecond
			if reason == "receive window" {
				resumeAt = 200 * time.Millisecond
				activeAt = 100 * time.Millisecond
			}
			paused.onSenderState(congestionSenderState{
				at: resumeAt, hasData: true, receiveWindowBytes: 65535,
			})
			paused.onACK(cubicTestACK(resumeAt, 1024))
			baseline.onACK(cubicTestACK(activeAt, 1024))
			if paused.windowSegments != baseline.windowSegments ||
				paused.clock.activeAt(resumeAt) != activeAt {
				t.Fatalf("pause changed growth: paused=%f baseline=%f", paused.windowSegments, baseline.windowSegments)
			}
		})
	}
}

// A full cwnd is normal ACK waiting, even if the workload would also impose a pause.
func TestCubicClockKeepsNetworkLimitedTime(t *testing.T) {
	policy := cubicTestPolicy(10)
	policy.onSenderState(congestionSenderState{
		at: 100 * time.Millisecond, readyAt: time.Second,
		hasData: true, flightBytes: 10 * 1024, receiveWindowBytes: 65535,
	})
	if policy.clock.limited || policy.clock.activeAt(300*time.Millisecond) != 300*time.Millisecond {
		t.Fatal("a full congestion window incorrectly paused network time")
	}
}
