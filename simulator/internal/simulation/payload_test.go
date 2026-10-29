package simulation

import (
	"load-balancer/model"
	"load-balancer/scenario"
	"testing"
	"time"
)

// payloadScenario spaces segments apart so reception and its ACK can be inspected separately.
func payloadScenario() scenario.Scenario {
	input := scenarioRuntimeDefinition()
	input.Resources.Backends = input.Resources.Backends[:1]
	input.Network.Defaults.BandwidthBitsPerSecond = 1_000_000_000
	input.Network.Defaults.PacketTravelTime = time.Millisecond
	input.Resources.LoadBalancer.Ingress = input.Network.Defaults
	input.Resources.LoadBalancer.Ingress.PacketTravelTime = 0
	input.Workload.Defaults.Bytes = 2050
	input.Workload.Defaults.PacketGap = 10 * time.Millisecond
	input.Execution.Duration = 2 * time.Second
	return input
}

// assertNativePayloadProgress compares native cumulative measurements.
func assertNativePayloadProgress(t *testing.T, world *World, received, acknowledged uint64) {
	t.Helper()
	want := PayloadProgress{ReceivedBytes: received, AcknowledgedBytes: acknowledged}
	if got := world.Info().Payload; got != want {
		t.Fatalf("payload progress = %+v, want %+v", got, want)
	}
}

// TestPayloadProgressAfterLostACK exercises a real data retransmission across modelled links.
func TestPayloadProgressAfterLostACK(t *testing.T) {
	s := New(normalizedRuntimeScenario(t, payloadScenario()))
	backend := s.lb.GetServers()[0]
	returnLink := scenarioRuntimeLink(t, s, BackendNodeID(backend.ID), LoadBalancerNodeID)
	discardedACK := false
	backend.SetSender(func(packet model.Packet) {
		// Discard just the first data ACK after its return-link transmission.
		discard := packet.Flag == model.FlagACK && !discardedACK
		if discard {
			discardedACK = true
		}
		originalClient := s.lb.GetClient(packet.Key)
		returnLink.Link.Transmit(packet.WireSize(), func() {
			if discard || !s.lb.HasClient(originalClient) {
				return
			}
			s.backendIngress.Transmit(packet.WireSize(), func() {
				if s.lb.HasClient(originalClient) {
					s.lb.HandleServerPacket(backend, packet)
				}
			})
		})
	})

	runIngressUntil(t, s, time.Second)
	assertNativePayloadProgress(t, s, 1024, 0)
	if !discardedACK || s.completedStreams != 0 {
		t.Fatal("the dropped ACK did not leave an unfinished transfer")
	}

	// The retry increases wire traffic while the received payload stays unchanged.
	runIngressUntil(t, s, 1017*time.Millisecond)
	assertNativePayloadProgress(t, s, 1024, 0)
	if s.clients[0].Client.GetState().RetryData != 1 {
		t.Fatal("the missing ACK did not trigger a data retransmission")
	}
	runIngressUntil(t, s, 1100*time.Millisecond)
	assertNativePayloadProgress(t, s, 2050, 2050)
	if s.completedPayloadBytes != 2050 {
		t.Fatal("the retry prevented completion or inflated the completed payload")
	}
	transfer := s.clients[0].Client.GetState().Transfer
	if transfer.Status != model.TransferSucceeded ||
		transfer.DataRetransmissions != 1 || transfer.SynRetransmissions != 0 {
		t.Fatalf("completed retry history was lost or double counted: %+v", transfer)
	}
	client := s.clients[0]
	uplink := scenarioRuntimeLink(t, s, client.ID, LoadBalancerNodeID).Stats()
	wantWireBytes := uint64(2050 + 1024 + 7*40)
	if uplink.DeliveredPackets != 7 || uplink.DeliveredBytes != wantWireBytes {
		t.Fatalf("wire counters did not retain the repeated payload and headers: %+v", uplink)
	}
}

// A small receive window changes segmentation; a lost ACK must not duplicate payload.
func TestSmallReceiveWindowCompletesAfterLostACK(t *testing.T) {
	input := payloadScenario()
	capacity := 64
	input.TCP.ReceiveBufferBytes = &capacity
	world := New(normalizedRuntimeScenario(t, input))
	backend := world.lb.GetServers()[0]
	returnLink := scenarioRuntimeLink(t, world, BackendNodeID(backend.ID), LoadBalancerNodeID)

	// Drop one data ACK after it travels through the backend's return link.
	droppedACK := false
	backend.SetSender(func(packet model.Packet) {
		discard := packet.Flag == model.FlagACK && !droppedACK
		if discard {
			droppedACK = true
		}
		originalClient := world.lb.GetClient(packet.Key)
		returnLink.Link.Transmit(packet.WireSize(), func() {
			if discard || !world.lb.HasClient(originalClient) {
				return
			}
			world.backendIngress.Transmit(packet.WireSize(), func() {
				if world.lb.HasClient(originalClient) {
					world.lb.HandleServerPacket(backend, packet)
				}
			})
		})
	})

	// Only the first 64 bytes reach the backend before the retransmission timeout.
	runIngressUntil(t, world, 20*time.Millisecond)
	assertNativePayloadProgress(t, world, 64, 0)
	if !droppedACK {
		t.Fatal("the first data ACK was not dropped")
	}

	// The complete 2050-byte stream fits into 33 segments, plus one retransmission.
	runIngressUntil(t, world, input.Execution.Duration)
	assertNativePayloadProgress(t, world, 2050, 2050)
	transfer := world.clients[0].Client.GetTransferState()
	if transfer.Status != model.TransferSucceeded || transfer.DataRetransmissions != 1 {
		t.Fatalf("small-window transfer did not recover: %+v", transfer)
	}

	runtimeClient := world.clients[0]
	uplink := scenarioRuntimeLink(t, world, runtimeClient.ID, LoadBalancerNodeID).Stats()
	const packets = 33 + 1 + 3 // Data, retransmission and SYN/ACK/FIN controls.
	const wireBytes = 2050 + 64 + packets*40
	if uplink.DeliveredPackets != packets || uplink.DeliveredBytes != wireBytes {
		t.Fatalf("unexpected small-window wire traffic: %+v", uplink)
	}
}
