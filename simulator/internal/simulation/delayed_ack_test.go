package simulation

import (
	"context"
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

// One MSS really is the congestion-window ceiling; rwnd stays at 65,535 bytes.
// Every packet must therefore wait for its own ACK, exposing the 40 ms timer.
func TestWorldDelayedACKWaitsWithOneSegmentWindow(t *testing.T) {
	for _, mode := range []string{"reno", "newReno", "tahoe", "cubic"} {
		t.Run(mode, func(t *testing.T) {
			var durations [2]time.Duration
			for index, delayed := range []bool{false, true} {
				definition := delayedACKWorldScenario(mode, delayed, 1)
				world := New(normalizedRuntimeScenario(t, definition))
				if err := world.Advance(context.Background(), world.EndTime()); err != nil {
					t.Fatal(err)
				}

				world.VisitClients(func(client ClientObservation) {
					state := client.State
					if state.Transfer.Status != model.TransferSucceeded {
						t.Fatalf("one-MSS transfer did not finish: %+v", state.Transfer)
					}
					if state.Congestion.CongestionWindowBytes != model.SEGMENT_SIZE {
						t.Fatalf("cwnd exceeded its one-MSS ceiling: %+v", state.Congestion)
					}
					durations[index] = state.Transfer.PayloadDuration
				})
				world.VisitBackends(func(backend BackendObservation) {
					ack := backend.State.ACK
					wantTimerACKs := uint64(0)
					if delayed {
						wantTimerACKs = 8
					}
					if ack.SentACKCount != 8 || ack.TimerTriggeredACKCount != wantTimerACKs {
						t.Fatalf("one segment at a time should retain one ACK per segment: %+v", ack)
					}
				})
			}

			// Eight segments each pay one complete virtual ACK delay.
			if durations[1] < durations[0]+8*40*time.Millisecond {
				t.Fatalf("missing delayed-ACK penalty: immediate=%v delayed=%v", durations[0], durations[1])
			}
		})
	}
}

// A wider window supplies the second segment without waiting for the first ACK.
func TestWorldDelayedACKGroupsPacketsWithRoomInWindow(t *testing.T) {
	definition := delayedACKWorldScenario("reno", true, 4)
	world := New(normalizedRuntimeScenario(t, definition))
	if err := world.Advance(context.Background(), world.EndTime()); err != nil {
		t.Fatal(err)
	}
	if world.Info().CompletedStreams != 1 {
		t.Fatal("the paired-ACK transfer did not complete")
	}
	world.VisitBackends(func(backend BackendObservation) {
		if state := backend.State.ACK; state.SentACKCount != 4 || state.TimerTriggeredACKCount != 0 {
			t.Fatalf("eight segments should form four immediate ACK pairs: %+v", state)
		}
	})
}

func delayedACKWorldScenario(mode string, enabled bool, windowSegments int) scenario.Scenario {
	definition := baseScenario()
	definition.Workload.ClientGroups[0].Count = 1
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 8 * model.SEGMENT_SIZE}
	definition.Resources.Backends = definition.Resources.Backends[:1]
	definition.TCP.CongestionControl.Mode = &mode
	definition.TCP.CongestionControl.MaximumWindowSegments = &windowSegments
	definition.TCP.DelayedACK.Enabled = &enabled
	return definition
}
