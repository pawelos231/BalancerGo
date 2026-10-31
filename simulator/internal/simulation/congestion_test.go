package simulation

import (
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

// congestionScenario keeps enough payload waiting to expose the sender's window.
func congestionScenario() scenario.Scenario {
	definition := baseScenario()
	definition.Execution.Duration = 2 * time.Second
	definition.Resources.Backends = definition.Resources.Backends[:1]
	definition.Workload.ClientGroups = []scenario.ClientGroup{{ID: "clients", Count: 1}}
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 12 * model.SEGMENT_SIZE}
	return definition
}

// interceptCongestionTraffic observes sends while retaining access and shared-input hops.
// Returning true drops the selected packet after those hops, before LB processing.
func interceptCongestionTraffic(t *testing.T, world *World, inspect func(model.Packet) bool) runtimeClient {
	t.Helper()
	runIngressUntil(t, world, 0)
	runtime := world.clients[0]
	access := scenarioRuntimeLink(t, world, runtime.ID, LoadBalancerNodeID)
	runtime.Client.SetSender(func(packet model.Packet) {
		discard := inspect(packet)
		access.Link.Transmit(packet.WireSize(), func() {
			world.clientIngress.Transmit(packet.WireSize(), func() {
				if !discard && world.lb.HasClient(runtime.Client) {
					world.lb.HandleClientPacket(packet)
				}
			})
		})
	})
	return runtime
}

// Dropping the first segment holds ACK at 1; Reno stops at 4 MSS while none keeps sending.
func TestWorldCongestionModeLimitsInitialBurst(t *testing.T) {
	for _, mode := range []string{"reno", "none"} {
		t.Run(mode, func(t *testing.T) {
			definition := congestionScenario()
			if mode == "none" {
				definition.TCP.CongestionControl.Mode = &mode
			}
			world := New(normalizedRuntimeScenario(t, definition))
			droppedFirst := false
			runtime := interceptCongestionTraffic(t, world, func(packet model.Packet) bool {
				if packet.Size > 0 && !droppedFirst {
					droppedFirst = true
					return true
				}
				return false
			})

			// This is before the 200 ms minimum RTO, so no retry has released the gap.
			runIngressUntil(t, world, 100*time.Millisecond)
			wantSegments := 4
			if mode == "none" {
				wantSegments = 12
			}
			state := runtime.Client.GetState()
			if !droppedFirst || state.Transfer.DataRetransmissions != 0 ||
				state.Congestion.BytesInFlight != uint64(wantSegments*model.SEGMENT_SIZE) {
				t.Fatalf("initial burst state = %+v", state)
			}
			if mode == "reno" && state.Congestion.CongestionWindowBytes != 4*model.SEGMENT_SIZE {
				t.Fatalf("default Reno window = %+v", state.Congestion)
			}
			assertNativePayloadProgress(t, world, uint64((wantSegments-1)*model.SEGMENT_SIZE), 0)
			access := scenarioRuntimeLink(t, world, runtime.ID, LoadBalancerNodeID)
			if access.Stats().DeliveredPackets < uint64(wantSegments) || world.Ingress().Stats.DeliveredPackets == 0 {
				t.Fatal("the burst did not traverse the physical topology")
			}
		})
	}
}

// A missing head leaves seven buffered tails; one RTO recovers all bytes without duplicates.
func TestWorldRenoLossUpdatesStateBeforeRetryAndCompletes(t *testing.T) {
	definition := congestionScenario()
	initialSegments := 8
	definition.TCP.CongestionControl.InitialWindowSegments = &initialSegments
	world := New(normalizedRuntimeScenario(t, definition))
	droppedFirst := false
	var retryState model.CongestionState
	var capturedRetry bool
	interceptCongestionTraffic(t, world, func(packet model.Packet) bool {
		if packet.Size == 0 {
			return false
		}
		if !droppedFirst {
			droppedFirst = true
			return true
		}
		if packet.Seq == 1 {
			// The sender callback runs before the retry is offered to any link.
			capturedRetry = true
			retryState = world.clients[0].Client.GetCongestionState()
		}
		return false
	})

	runIngressUntil(t, world, 100*time.Millisecond)
	assertNativePayloadProgress(t, world, 7*model.SEGMENT_SIZE, 0)
	if capturedRetry {
		t.Fatal("the retry ran before its timeout")
	}

	runIngressUntil(t, world, definition.Execution.Duration)
	if !capturedRetry || retryState.CongestionWindowBytes != model.SEGMENT_SIZE ||
		retryState.SlowStartThresholdBytes != 4*model.SEGMENT_SIZE ||
		retryState.BytesInFlight != 8*model.SEGMENT_SIZE {
		t.Fatalf("state visible before retransmission = %+v", retryState)
	}
	assertNativePayloadProgress(t, world, 12*model.SEGMENT_SIZE, 12*model.SEGMENT_SIZE)
	state := world.clients[0].Client.GetState()
	if state.Transfer.Status != model.TransferSucceeded || state.Transfer.DataRetransmissions != 1 ||
		state.Congestion.BytesInFlight != 0 {
		t.Fatalf("recovered transfer = %+v", state)
	}
}
