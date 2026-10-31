package simulation

import (
	"testing"
	"time"

	"load-balancer/model"
)

// Drop [1,1025) once: later segments create duplicate ACKs through the full network.
// Enabled Reno recovers before 100 ms; disabling it preserves the 200 ms RTO path.
func TestWorldFastRecoveryUsesDuplicateACKsAcrossPhysicalLinks(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		name := "timeout only"
		if enabled {
			name = "fast recovery"
		}

		t.Run(name, func(t *testing.T) {
			definition := congestionScenario()
			initialSegments := 8
			definition.TCP.CongestionControl.InitialWindowSegments = &initialSegments
			definition.TCP.CongestionControl.FastRetransmit = &enabled
			world := New(normalizedRuntimeScenario(t, definition))

			droppedFirst := false
			var retriedAt time.Duration
			var retryState model.CongestionState
			interceptCongestionTraffic(t, world, func(packet model.Packet) bool {
				if packet.Size == 0 {
					return false
				}
				if !droppedFirst {
					droppedFirst = true
					return true
				}

				// Observe the prepared recovery state before the retry enters any link.
				if packet.Seq == 1 && retriedAt == 0 {
					retriedAt = world.Now()
					retryState = world.clients[0].Client.GetCongestionState()
				}
				return false
			})

			runIngressUntil(t, world, 100*time.Millisecond)
			if enabled {
				if retriedAt == 0 || retriedAt >= 100*time.Millisecond {
					t.Fatalf("fast retry at %s did not beat the RTO", retriedAt)
				}
				if retryState.Phase != "fastRecovery" || retryState.FastRetransmissions != 1 ||
					retryState.CongestionWindowBytes != 7*model.SEGMENT_SIZE ||
					retryState.SlowStartThresholdBytes != 4*model.SEGMENT_SIZE ||
					retryState.BytesInFlight != 8*model.SEGMENT_SIZE {
					t.Fatalf("state before fast retry = %+v", retryState)
				}
				assertNativePayloadProgress(t, world, 12*model.SEGMENT_SIZE, 12*model.SEGMENT_SIZE)
			} else if retriedAt != 0 {
				t.Fatalf("disabled fast recovery retried at %s", retriedAt)
			}

			// Both policies eventually recover the same unique payload, without double counting.
			runIngressUntil(t, world, world.EndTime())
			state := world.clients[0].Client.GetState()
			if state.Transfer.Status != model.TransferSucceeded || state.Transfer.DataRetransmissions != 1 {
				t.Fatalf("finished transfer = %+v", state)
			}
			wantFastRetries := uint64(0)
			if enabled {
				wantFastRetries = 1
			}
			if state.Congestion.FastRetransmissions != wantFastRetries {
				t.Fatalf("fast retries = %d, want %d", state.Congestion.FastRetransmissions, wantFastRetries)
			}
			assertNativePayloadProgress(t, world, 12*model.SEGMENT_SIZE, 12*model.SEGMENT_SIZE)
		})
	}
}
