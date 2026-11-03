package congestioncontrol

import (
	"testing"
	"time"
)

// Inserting a short workload pause must leave the same cubic window at the same active time.
func TestCubicClockExcludesKnownGapAndReceiverPause(t *testing.T) {
	for _, reason := range []string{"workload gap", "receive window"} {
		t.Run(reason, func(t *testing.T) {
			baseline, paused := cubicTestPolicy(10), cubicTestPolicy(10)
			baseline.OnACK(cubicTestACK(0, 1024))
			paused.OnACK(cubicTestACK(0, 1024))
			sender := SenderState{
				At: 100 * time.Millisecond, HasData: true, ReceiveWindowBytes: 65535,
			}
			if reason == "workload gap" {
				sender.ReadyAt = 200 * time.Millisecond
			} else {
				sender.ReceiveWindowBytes = 0
			}
			paused.OnSenderState(sender)
			paused.OnACK(cubicTestACK(150*time.Millisecond, 1024))
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
			paused.OnSenderState(SenderState{
				At: resumeAt, HasData: true, ReceiveWindowBytes: 65535,
			})
			paused.OnACK(cubicTestACK(resumeAt, 1024))
			baseline.OnACK(cubicTestACK(activeAt, 1024))
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
	policy.OnSenderState(SenderState{
		At: 100 * time.Millisecond, ReadyAt: time.Second,
		HasData: true, FlightBytes: 10 * 1024, ReceiveWindowBytes: 65535,
	})
	if policy.clock.limited || policy.clock.activeAt(300*time.Millisecond) != 300*time.Millisecond {
		t.Fatal("a full congestion window incorrectly paused network time")
	}
}
