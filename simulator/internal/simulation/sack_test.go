package simulation

import (
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

// Drop original segments 2 and 4 from an eight-segment stream, with no new data behind it.
// SACK repairs both holes before the first repair ACK is even emitted by the backend.
func TestWorldSACKRepairsTwoHolesBeforeWaitingForTheFirstRepairACK(t *testing.T) {
	for _, mode := range []string{"reno", "newReno", "cubic"} {
		t.Run(mode, func(t *testing.T) {
			world := newTimelineWorld(t, sackIntegrationScenario(mode, true))
			attempts := make(map[uint64]int)
			retryTimes := make(map[uint64]time.Duration)
			var firstRepairACKAt time.Duration
			clientPackets := 0
			sawPermission := false
			sawRangeReport := false

			interceptClosingTraffic(t, world, func(fromClient bool, packet model.Packet) bool {
				if !fromClient {
					sawPermission = sawPermission || packet.SACKPermitted
					sawRangeReport = sawRangeReport || len(packet.SACKBlocks) != 0
					if packet.Flag == model.FlagACK && packet.Ack > 1025 && firstRepairACKAt == 0 {
						firstRepairACKAt = world.Now()
					}
					return false
				}

				clientPackets++
				if packet.Size == 0 {
					return false
				}
				attempts[packet.Seq]++
				missing := packet.Seq == 1025 || packet.Seq == 3073
				if missing && attempts[packet.Seq] == 1 {
					return true
				}
				if missing && attempts[packet.Seq] == 2 {
					retryTimes[packet.Seq] = world.Now()
				}
				return false
			})

			// Finish before the 200 ms RTO floor, including a short, explicit TIME_WAIT.
			runIngressUntil(t, world, 100*time.Millisecond)
			client := world.clients[0].Client
			state := client.GetState()
			if !sawPermission || !sawRangeReport || firstRepairACKAt == 0 {
				t.Fatalf(
					"SACK transfer lacked negotiation, ranges or repair ACK: permitted=%v, ranges=%v, repair ACK=%s",
					sawPermission, sawRangeReport, firstRepairACKAt,
				)
			}
			for _, sequence := range []uint64{1025, 3073} {
				retriedAt := retryTimes[sequence]
				if retriedAt == 0 || retriedAt >= firstRepairACKAt {
					t.Fatalf(
						"segment at Seq=%d was retried at %s, first repair ACK was emitted at %s",
						sequence, retriedAt, firstRepairACKAt,
					)
				}
			}
			if !client.IsTerminated() || state.Connection.Outcome != model.CloseGraceful ||
				state.Transfer.Status != model.TransferSucceeded ||
				state.Transfer.DataRetransmissions < 2 || state.Transfer.DataRetransmissions > 3 ||
				state.Congestion.FastRetransmissions != state.Transfer.DataRetransmissions {
				t.Fatalf(
					"two SACK repairs did not complete and close cleanly: state=%+v, attempts=%v",
					state, attempts,
				)
			}
			// RFC 6675 also permits one rescue retry after a partial ACK, even if a repair is in flight.
			assertNativePayloadProgress(t, world, 8*model.SEGMENT_SIZE, 8*model.SEGMENT_SIZE)

			// Passing every old data timeout must not cause another send after closure.
			packetsAtClose := clientPackets
			runIngressUntil(t, world, world.EndTime())
			if clientPackets != packetsAtClose || client.GetTransferState() != state.Transfer {
				t.Fatal("the closed SACK sender emitted another packet or changed its transfer result")
			}
		})
	}
}

// Without SACK, Reno sees only one missing position at a time.
// The same finite stream repairs segment 2 quickly, then needs RTO for segment 4.
func TestWorldSACKDisabledKeepsClassicRenoRecovery(t *testing.T) {
	world := newTimelineWorld(t, sackIntegrationScenario("reno", false))
	attempts := make(map[uint64]int)
	var secondRepairAt time.Duration
	sawSACK := false
	interceptClosingTraffic(t, world, func(fromClient bool, packet model.Packet) bool {
		sawSACK = sawSACK || packet.SACKPermitted || len(packet.SACKBlocks) != 0
		if !fromClient || packet.Size == 0 {
			return false
		}
		attempts[packet.Seq]++
		missing := packet.Seq == 1025 || packet.Seq == 3073
		if missing && attempts[packet.Seq] == 1 {
			return true
		}
		if packet.Seq == 3073 && attempts[packet.Seq] == 2 {
			secondRepairAt = world.Now()
		}
		return false
	})

	runIngressUntil(t, world, 100*time.Millisecond)
	state := world.clients[0].Client.GetState()
	if sawSACK || state.Transfer.Status != model.TransferInProgress ||
		state.Transfer.DataRetransmissions != 1 || secondRepairAt != 0 {
		t.Fatalf("SACK-off Reno did not retain its original single fast retry: %+v", state)
	}

	// Once its ordinary RTO fires, the second missing segment is also repaired.
	runIngressUntil(t, world, world.EndTime())
	state = world.clients[0].Client.GetState()
	if secondRepairAt < 200*time.Millisecond || state.Transfer.Status != model.TransferSucceeded ||
		state.Transfer.DataRetransmissions != 2 || state.Congestion.FastRetransmissions != 1 {
		t.Fatalf("SACK-off Reno did not complete through its original RTO path: %+v", state)
	}
	assertNativePayloadProgress(t, world, 8*model.SEGMENT_SIZE, 8*model.SEGMENT_SIZE)
}

// Eight clients cross independently seeded lossy access links and one real simulated LB.
// Their retries must preserve exact unique payload totals and eventually finish closing.
func TestWorldSACKCompletesSeededLossyAccessTransfers(t *testing.T) {
	for _, mode := range []string{"reno", "newReno", "cubic"} {
		t.Run(mode, func(t *testing.T) {
			definition := sackIntegrationScenario(mode, true)
			definition.Fixed.Execution.Seed = 19
			definition.Fixed.Execution.Duration = 10 * time.Second
			editInitialState(&definition, func(state *scenario.StateConfig) { state.Workload.ClientGroups[0].Count = 8 })
			definition.Fixed.Workload.Defaults.Bytes = 32 * model.SEGMENT_SIZE
			loss := 0.04
			editInitialState(&definition, func(state *scenario.StateConfig) {
				state.Workload.ClientGroups[0].Network.ToLoadBalancer.LossRate = &loss
			})
			world := newTimelineWorld(t, definition)

			runIngressUntil(t, world, world.EndTime())
			var retries uint64
			for _, runtime := range world.clients {
				state := runtime.Client.GetState()
				retries += state.Transfer.DataRetransmissions
				if state.Transfer.Status != model.TransferSucceeded || !runtime.Client.IsTerminated() ||
					state.Connection.Outcome != model.CloseGraceful {
					t.Fatalf("seeded lossy client %s did not finish gracefully: %+v", runtime.ID, state)
				}
			}
			if retries == 0 {
				t.Fatal("the seeded fixture did not exercise any lost payload")
			}
			wantPayload := uint64(8 * 32 * model.SEGMENT_SIZE)
			assertNativePayloadProgress(t, world, wantPayload, wantPayload)
		})
	}
}

// Eight segments fill the initial window; a short TIME_WAIT lets tests observe full closure.
func sackIntegrationScenario(mode string, enabled bool) scenario.ScenarioV2 {
	definition := congestionScenario()
	initialSegments := 8
	msl := 5 * time.Millisecond
	definition.Fixed.Workload.Defaults.Bytes = 8 * model.SEGMENT_SIZE
	definition.Fixed.TCP.CongestionControl.Mode = &mode
	definition.Fixed.TCP.CongestionControl.InitialWindowSegments = &initialSegments
	definition.Fixed.TCP.SACKEnabled = &enabled
	definition.Fixed.TCP.Close.MSL = &msl
	return definition
}
