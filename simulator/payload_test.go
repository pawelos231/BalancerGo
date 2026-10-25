package main

import (
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
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

// assertPayloadProgress checks the run totals and their headless representation.
func assertPayloadProgress(t *testing.T, s *simulation, received, acknowledged uint64) {
	t.Helper()
	want := payloadProgress{ReceivedBytes: received, AcknowledgedBytes: acknowledged}
	if s.payload != want {
		t.Fatalf("payload progress = %+v, want %+v", s.payload, want)
	}
	result := s.result(0)
	if result.ReceivedPayloadBytes != received || result.AcknowledgedPayloadBytes != acknowledged {
		t.Fatalf("headless payload progress differs from the live totals: %+v", result)
	}
}

// TestPayloadProgressIncludesUnfinishedTransfers keeps the two measurement boundaries distinct.
func TestPayloadProgressIncludesUnfinishedTransfers(t *testing.T) {
	s := newScenarioSimulation(normalizedRuntimeScenario(t, payloadScenario()))
	publisher := newTelemetryPublisher("payload-run")
	runIngressUntil(t, s, 5*time.Millisecond)
	first := sampleTelemetry(t, publisher, s)
	assertPayloadProgress(t, s, 0, 0) // Only handshake traffic has arrived.

	// A segment has reached the backend while its ACK is still travelling.
	runIngressUntil(t, s, 17*time.Millisecond)
	assertPayloadProgress(t, s, 1024, 0)
	if s.completedStreams != 0 || s.completedPayloadBytes != 0 {
		t.Fatal("partial progress was counted as a completed stream")
	}
	received := sampleTelemetry(t, publisher, s)
	if received.Document != nil || received.Topology.Revision != first.Topology.Revision {
		t.Fatal("payload changes rebuilt the topology definitions")
	}
	if received.Topology.Nodes[0].Metrics["receivedPayloadBytes"] != 1024 ||
		received.Topology.Nodes[0].Metrics["acknowledgedPayloadBytes"] != 0 ||
		received.Servers[0].ReceivedPayloadBytes != 1024 {
		t.Fatal("the live snapshot lost partial received payload")
	}

	// The same data is now ACK-confirmed, but the transfer remains incomplete.
	runIngressUntil(t, s, 19*time.Millisecond)
	assertPayloadProgress(t, s, 1024, 1024)
	confirmed := sampleTelemetry(t, publisher, s)
	if confirmed.Servers[0].Clients[0].AcknowledgedPayloadBytes != 1024 {
		t.Fatal("routed-client telemetry lost confirmed payload")
	}
	clientNode := confirmed.Topology.Nodes[len(confirmed.Topology.Nodes)-1]
	if clientNode.Metrics["acknowledgedPayloadBytes"] != 1024 {
		t.Fatal("client topology lost confirmed payload")
	}
	if s.completedPayloadBytes != 0 || received.Servers[0].ReceivedPayloadBytes != 1024 {
		t.Fatal("a later observation changed completion accounting or an earlier snapshot")
	}

	// The final segment is only two bytes; closure and removal retain historical totals.
	runIngressUntil(t, s, 60*time.Millisecond)
	assertPayloadProgress(t, s, 2050, 2050)
	if s.completedStreams != 1 || !s.clients[0].Client.IsClosed() {
		t.Fatal("the transfer did not complete and close")
	}
	backend := s.lb.GetServers()[0]
	if backend.GetState().ReceivedPayloadBytes != 2050 {
		t.Fatal("connection cleanup erased the backend payload total")
	}
	s.lb.RemoveServer(backend)
	assertPayloadProgress(t, s, 2050, 2050)
	fresh := newScenarioSimulation(s.scenario)
	assertPayloadProgress(t, fresh, 0, 0)
}

// TestPayloadProgressAfterLostACK exercises a real data retransmission across modelled links.
func TestPayloadProgressAfterLostACK(t *testing.T) {
	s := newScenarioSimulation(normalizedRuntimeScenario(t, payloadScenario()))
	backend := s.lb.GetServers()[0]
	returnLink := scenarioRuntimeLink(t, s, backendNodeID(backend.ID), loadBalancerNodeID)
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
	assertPayloadProgress(t, s, 1024, 0)
	if !discardedACK || s.completedStreams != 0 {
		t.Fatal("the dropped ACK did not leave an unfinished transfer")
	}

	// The retry increases wire traffic while the received payload stays unchanged.
	runIngressUntil(t, s, 1017*time.Millisecond)
	assertPayloadProgress(t, s, 1024, 0)
	if s.clients[0].Client.GetState().RetryData != 1 {
		t.Fatal("the missing ACK did not trigger a data retransmission")
	}
	runIngressUntil(t, s, 1100*time.Millisecond)
	assertPayloadProgress(t, s, 2050, 2050)
	if s.completedPayloadBytes != 2050 {
		t.Fatal("the retry prevented completion or inflated the completed payload")
	}
	transfers := s.transferResults()
	if transfers.Succeeded != 1 || transfers.DataRetransmissions != 1 || transfers.SynRetransmissions != 0 {
		t.Fatalf("completed retry history was lost or double counted: %+v", transfers)
	}
	client := s.clients[0]
	uplink := scenarioRuntimeLink(t, s, client.ID, loadBalancerNodeID).Stats()
	wantWireBytes := uint64(2050 + 1024 + 7*40)
	if uplink.DeliveredPackets != 7 || uplink.DeliveredBytes != wantWireBytes {
		t.Fatalf("wire counters did not retain the repeated payload and headers: %+v", uplink)
	}
}

// TestPayloadSnapshotLifecycle checks actual WS totals, Stop and a fresh run.
func TestPayloadSnapshotLifecycle(t *testing.T) {
	h := newScenarioHarness(t)
	idle := readScenarioSnapshot(t, h.socket, func(snapshot simulationSnapshot) bool {
		return snapshot.Simulation.Status == "idle"
	})
	if idle.Simulation.Payload != nil {
		t.Fatal("idle snapshot reported payload from a nonexistent run")
	}

	input := payloadScenario()
	input.Execution.Speed = 0
	receipt := startTelemetryScenario(t, h, input)
	finished := readScenarioSnapshot(t, h.socket, func(snapshot simulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "finished"
	})
	want := payloadProgress{ReceivedBytes: 2050, AcknowledgedBytes: 2050}
	if finished.Simulation.Payload == nil || *finished.Simulation.Payload != want {
		t.Fatalf("finished WS payload = %+v, want %+v", finished.Simulation.Payload, want)
	}
	stopTelemetryScenario(t, h)

	// A new run ends before handshake completion and must start its own counters at zero.
	input.Execution.Duration = time.Microsecond
	input.Execution.SnapshotInterval = time.Microsecond
	second := startTelemetryScenario(t, h, input)
	fresh := readScenarioSnapshot(t, h.socket, func(snapshot simulationSnapshot) bool {
		return snapshot.Simulation.RunID == second.RunID && snapshot.Simulation.Status == "finished"
	})
	if fresh.Simulation.Payload == nil || *fresh.Simulation.Payload != (payloadProgress{}) {
		t.Fatalf("fresh WS payload retained earlier work: %+v", fresh.Simulation.Payload)
	}
	if *finished.Simulation.Payload != want {
		t.Fatal("a new run changed the previous detached payload totals")
	}
	stopTelemetryScenario(t, h)
}
