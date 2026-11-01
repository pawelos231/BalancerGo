package simulation

import (
	"testing"
	"time"

	"load-balancer/model"
)

// Drop [1,1025) once: later segments create duplicate ACKs through the full network.
// Each algorithm repairs it before RTO, but exposes its own window at the retry.
func TestWorldCongestionStrategiesRecoverSamePayload(t *testing.T) {
	tests := []struct {
		name           string
		mode           string
		fastRetransmit bool
		retryWindow    uint64
		retryThreshold uint64
		retryPhase     string
	}{
		{
			name: "Reno fast recovery", mode: "reno", fastRetransmit: true,
			retryWindow: 7 * model.SEGMENT_SIZE, retryThreshold: 4 * model.SEGMENT_SIZE,
			retryPhase: "fastRecovery",
		},
		{
			name: "Tahoe slow start", mode: "tahoe", fastRetransmit: true,
			retryWindow: model.SEGMENT_SIZE, retryThreshold: 4 * model.SEGMENT_SIZE,
			retryPhase: "slowStart",
		},
		{
			// 70% of 8192 bytes rounds down to 5734; recovery adds three MSS.
			name: "CUBIC fast recovery", mode: "cubic", fastRetransmit: true,
			retryWindow: 5734 + 3*model.SEGMENT_SIZE, retryThreshold: 5734,
			retryPhase: "fastRecovery",
		},
		{name: "Reno timeout only", mode: "reno"},
		{name: "Tahoe timeout only", mode: "tahoe"},
		{name: "CUBIC timeout only", mode: "cubic"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := congestionScenario()
			initialSegments := 8
			definition.TCP.CongestionControl.Mode = &test.mode
			definition.TCP.CongestionControl.InitialWindowSegments = &initialSegments
			definition.TCP.CongestionControl.FastRetransmit = &test.fastRetransmit
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
			if test.fastRetransmit {
				if retriedAt == 0 || retriedAt >= 100*time.Millisecond {
					t.Fatalf("fast retry at %s did not beat the RTO", retriedAt)
				}
				if retryState.Mode != model.CongestionMode(test.mode) ||
					retryState.Phase != test.retryPhase || retryState.FastRetransmissions != 1 ||
					retryState.CongestionWindowBytes != test.retryWindow ||
					retryState.SlowStartThresholdBytes != test.retryThreshold ||
					retryState.BytesInFlight != 8*model.SEGMENT_SIZE {
					t.Fatalf("state before fast retry = %+v", retryState)
				}
				assertNativePayloadProgress(t, world, 12*model.SEGMENT_SIZE, 12*model.SEGMENT_SIZE)
			} else if retriedAt != 0 {
				t.Fatalf("disabled fast retransmit retried at %s", retriedAt)
			}

			// Both policies eventually recover the same unique payload, without double counting.
			runIngressUntil(t, world, world.EndTime())
			state := world.clients[0].Client.GetState()
			if state.Transfer.Status != model.TransferSucceeded || state.Transfer.DataRetransmissions != 1 {
				t.Fatalf("finished transfer = %+v", state)
			}
			wantFastRetries := uint64(0)
			if test.fastRetransmit {
				wantFastRetries = 1
			}
			if state.Congestion.FastRetransmissions != wantFastRetries {
				t.Fatalf("fast retries = %d, want %d", state.Congestion.FastRetransmissions, wantFastRetries)
			}
			assertNativePayloadProgress(t, world, 12*model.SEGMENT_SIZE, 12*model.SEGMENT_SIZE)
		})
	}
}
