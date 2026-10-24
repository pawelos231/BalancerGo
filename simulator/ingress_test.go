package main

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/client"
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
)

// ingressTestConfiguration keeps packet timings exact and retries out of range.
func ingressTestConfiguration() configuration {
	cfg := legacyTestConfiguration()
	cfg.clients = 1
	cfg.servers = 1
	cfg.streamBytes = 0
	cfg.bandwidth = 8_000
	cfg.forwardingBitsPerSecond = math.MaxInt64
	cfg.packetTravelTime = 3 * time.Millisecond
	cfg.duration = 500 * time.Millisecond
	cfg.ingress = scenario.LinkConfig{
		BandwidthBitsPerSecond: 4_000,
		PacketTravelTime:       7 * time.Millisecond,
		QueueCapacity:          16,
	}
	return cfg
}

// runIngressUntil advances only virtual time, without starting HTTP or pacing.
func runIngressUntil(t *testing.T, s *simulation, until time.Duration) {
	t.Helper()
	if err := s.engine.RunUntil(until); err != nil {
		t.Fatal(err)
	}
}

// firstIngressClient returns the first client created by production wiring.
func firstIngressClient(t *testing.T, s *simulation) *client.Client {
	t.Helper()
	if len(s.clients) == 0 {
		t.Fatal("production client has not been created")
	}
	return s.clients[0].Client
}

// firstIngressClientOutput finds the first client's private LB output FIFO.
func firstIngressClientOutput(t *testing.T, s *simulation) *network.Output {
	t.Helper()
	firstIngressClient(t, s)
	id := s.clients[0].ID
	for _, link := range s.links {
		if link.Source == loadBalancerNodeID && link.Target == id {
			if link.Output == nil {
				t.Fatal("client output has no managed FIFO")
			}
			return link.Output
		}
	}
	t.Fatalf("client %q has no LB output", id)
	return nil
}

// ingressClientAccessDeliveries sums successful arrivals from all private links.
func ingressClientAccessDeliveries(s *simulation) uint64 {
	var deliveries uint64
	for _, cl := range s.clients {
		for _, link := range s.links {
			if link.Source == cl.ID && link.Target == loadBalancerNodeID {
				deliveries += link.Stats().DeliveredPackets
			}
		}
	}
	return deliveries
}

// TestIngressDelaysBothClientAndBackendPackets checks both real sender callbacks.
func TestIngressDelaysBothClientAndBackendPackets(t *testing.T) {
	cfg := ingressTestConfiguration()
	s := newSimulation(cfg)
	srv := s.lb.GetServers()[0]

	// A SYN has 40 wire bytes: 40 ms access transmission plus 3 ms travel.
	runIngressUntil(t, s, 43*time.Millisecond-time.Nanosecond)
	if s.ingress.Stats().OfferedPackets != 0 {
		t.Fatal("SYN entered ingress before its access link finished")
	}
	runIngressUntil(t, s, 43*time.Millisecond)
	input := s.ingress.Stats()
	if input.AcceptedPackets != 1 || input.AcceptedBytes != 40 {
		t.Fatalf("access arrival did not offer the complete SYN wire size: %+v", input)
	}
	if s.egress.Stats().ForwardedPackets != 0 || len(s.lb.GetConnectionTable()[srv]) != 0 {
		t.Fatal("SYN bypassed ingress and reached LB routing")
	}

	// Ingress adds 80 ms serialization and 7 ms travel, independently of access.
	runIngressUntil(t, s, 130*time.Millisecond-time.Nanosecond)
	if s.egress.Stats().ForwardedPackets != 0 {
		t.Fatal("LB handled the SYN before ingress delivery")
	}
	runIngressUntil(t, s, 130*time.Millisecond)
	if len(s.lb.GetConnectionTable()[srv]) != 1 {
		t.Fatal("ingress delivery did not route the SYN")
	}
	forwarding := s.egress.Stats()
	if forwarding.ForwardedPackets != 1 || forwarding.ForwardedBytes != 40 {
		t.Fatalf("LB output did not start the complete SYN wire packet: %+v", forwarding)
	}
	if srv.NumberOfHandledPackets != 0 {
		t.Fatal("backend received the SYN before its outgoing link completed")
	}

	// The backend receives the SYN at 173 ms; its SYN-ACK reaches its input queue at 216 ms.
	runIngressUntil(t, s, 216*time.Millisecond)
	if srv.NumberOfHandledPackets != 1 {
		t.Fatal("backend did not receive the forwarded SYN")
	}
	input = s.ingress.Stats()
	if input.OfferedPackets != 2 || input.OfferedBytes != 80 {
		t.Fatalf("backend reply did not join the same physical ingress: %+v", input)
	}
	if firstIngressClientOutput(t, s).Stats().OfferedPackets != 0 {
		t.Fatal("backend reply bypassed ingress on its way to the client")
	}

	// The reply pays the same ingress cost before the LB can send it onward.
	runIngressUntil(t, s, 303*time.Millisecond-time.Nanosecond)
	if firstIngressClientOutput(t, s).Stats().OfferedPackets != 0 {
		t.Fatal("LB handled the backend reply before its ingress delivery")
	}
	runIngressUntil(t, s, 303*time.Millisecond)
	forwarding = s.egress.Stats()
	if forwarding.ForwardedPackets != 2 || forwarding.ForwardedBytes != 80 {
		t.Fatalf("backend reply did not consume one outgoing grant: %+v", forwarding)
	}
	if s.ingress.Stats().DeliveredPackets != 2 {
		t.Fatal("both input directions did not complete the common link")
	}
	clientDeliveries := s.clientIngress.Stats().DeliveredPackets
	backendDeliveries := s.backendIngress.Stats().DeliveredPackets
	if clientDeliveries != 1 || backendDeliveries != 1 {
		t.Fatal("input queues did not retain their own completed packet counters")
	}
	if firstIngressClient(t, s).IsHandshakeCompleted() {
		t.Fatal("client received SYN-ACK before its outgoing link completed")
	}
}

// TestIngressClientBurstUsesOneFiniteFIFO checks clients sharing their input FIFO.
func TestIngressClientBurstUsesOneFiniteFIFO(t *testing.T) {
	for _, capacity := range []int{0, 1} {
		name := "no waiting slots"
		if capacity == 1 {
			name = "one waiting slot"
		}
		t.Run(name, func(t *testing.T) {
			cfg := ingressTestConfiguration()
			cfg.clients = 3
			cfg.bandwidth = 320_000
			cfg.packetTravelTime = 0
			cfg.duration = 10 * time.Millisecond
			cfg.ingress.BandwidthBitsPerSecond = 8_000
			cfg.ingress.QueueCapacity = capacity
			s := newSimulation(cfg)

			// Private access links deliver all three SYNs at 1 ms; ingress needs 40 ms each.
			if err := s.run(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			accepted := uint64(1 + capacity)
			dropped := uint64(3) - accepted
			want := network.Stats{
				OfferedPackets:      3,
				OfferedBytes:        120,
				AcceptedPackets:     accepted,
				AcceptedBytes:       accepted * 40,
				QueueDroppedPackets: dropped,
				QueueDroppedBytes:   dropped * 40,
				QueuedPackets:       capacity,
				Transmitting:        true,
			}
			if got := s.result(0).Ingress.Stats; got != want {
				t.Fatalf("shared ingress = %+v, want %+v", got, want)
			}
			if ingressClientAccessDeliveries(s) != 3 {
				t.Fatal("client SYNs were dropped before reaching ingress")
			}
			if s.egress.Stats().ForwardedPackets != 0 {
				t.Fatal("waiting or dropped SYNs reached an LB packet handler")
			}
		})
	}
}

// TestIngressQueuesShareOneReceiver checks isolation without multiplying bandwidth.
func TestIngressQueuesShareOneReceiver(t *testing.T) {
	cfg := ingressTestConfiguration()
	cfg.bandwidth = 320_000
	cfg.packetTravelTime = 0
	cfg.ingress = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000,
		QueueCapacity:          1,
	}
	definition := cfg.scenarioDefinition()
	definition.Workload.ClientGroups = []scenario.ClientGroup{
		{ID: "initial", Count: 2},
		{
			ID:      "later",
			Count:   2,
			StartAt: 43 * time.Millisecond,
		},
	}
	s := newScenarioSimulation(normalizedRuntimeScenario(t, definition))

	// Two initial SYNs share one transmitter: one starts and the other waits.
	runIngressUntil(t, s, time.Millisecond)
	if s.clientIngress.Stats().QueuedPackets != 1 {
		t.Fatal("initial client burst did not fill the client input queue")
	}
	if s.backendIngress.Stats() != (network.Stats{}) {
		t.Fatal("client SYNs entered the backend input queue")
	}

	// The first real SYN-ACK reaches its own queue at 43 ms, behind a busy receiver.
	runIngressUntil(t, s, 43*time.Millisecond)
	backend := s.backendIngress.Stats()
	if backend.AcceptedPackets != 1 || backend.QueuedPackets != 1 {
		t.Fatalf("backend reply did not retain its own waiting slot: %+v", backend)
	}
	if firstIngressClientOutput(t, s).Stats().OfferedPackets != 0 {
		t.Fatal("backend reply bypassed the shared receiver")
	}

	// New SYNs can fill the client queue while the backend queue stays full.
	runIngressUntil(t, s, 44*time.Millisecond)
	want := network.Stats{
		OfferedPackets:      5,
		OfferedBytes:        200,
		AcceptedPackets:     4,
		AcceptedBytes:       160,
		QueueDroppedPackets: 1,
		QueueDroppedBytes:   40,
		DeliveredPackets:    1,
		DeliveredBytes:      40,
		TransmittedBytes:    40,
		QueuedPackets:       2,
		Transmitting:        true,
	}
	if got := s.ingress.Stats(); got != want {
		t.Fatalf("shared receiver = %+v, want %+v", got, want)
	}
	clients := s.clientIngress.Stats()
	if clients.QueuedPackets != 1 || clients.QueueDroppedPackets != 1 {
		t.Fatalf("client input did not enforce its own capacity: %+v", clients)
	}
	if s.backendIngress.Stats() != backend {
		t.Fatal("client overload changed the waiting backend reply")
	}
	assertIngressSnapshots(t, s)

	// The older backend head starts at 81 ms, after two 40 ms transmissions.
	runIngressUntil(t, s, 81*time.Millisecond)
	backend = s.backendIngress.Stats()
	if !backend.Transmitting || backend.QueuedPackets != 0 {
		t.Fatalf("the older backend head did not receive the next grant: %+v", backend)
	}
	if s.clientIngress.Stats().QueuedPackets != 1 {
		t.Fatal("a newer client head overtook the waiting backend reply")
	}

	// One physical receiver makes the reply wait until 121 ms before LB delivery.
	runIngressUntil(t, s, 121*time.Millisecond-time.Nanosecond)
	if firstIngressClientOutput(t, s).Stats().OfferedPackets != 0 {
		t.Fatal("independent input queues multiplied the physical ingress bandwidth")
	}
	runIngressUntil(t, s, 121*time.Millisecond)
	if firstIngressClientOutput(t, s).Stats().OfferedPackets != 1 {
		t.Fatal("the backend reply did not reach the client output at 121 ms")
	}
}

// assertIngressSnapshots checks separate input queues and a detached shared resource.
func assertIngressSnapshots(t *testing.T, s *simulation) {
	t.Helper()
	report := s.result(0)
	graph := s.topology(report.State)
	aggregate := s.ingress.Stats()
	if graph.Ingress == nil {
		t.Fatal("topology omitted the shared ingress resource")
	}
	if report.Ingress.Name != "Shared LB input" || report.Ingress.Stats != aggregate {
		t.Fatalf("report omitted aggregate ingress counters: %+v", report.Ingress)
	}
	if graph.Ingress.Stats != topologyStatsForTest(aggregate) {
		t.Fatalf("topology omitted aggregate ingress counters: %+v", graph.Ingress.Stats)
	}
	if len(report.IngressQueues) != 2 || len(graph.Ingress.Queues) != 2 {
		t.Fatal("report and topology must expose exactly two input queues")
	}

	// Queue capacity belongs to each side; the common resource carries the rate once.
	capacity := s.ingress.Config().QueueCapacity
	wantQueues := map[string]network.Stats{
		"clients":  s.clientIngress.Stats(),
		"backends": s.backendIngress.Stats(),
	}
	seenSides := make(map[string]bool)
	seenIDs := make(map[string]bool)
	for _, queue := range graph.Ingress.Queues {
		want, exists := wantQueues[queue.Side]
		if !exists || seenSides[queue.Side] {
			t.Fatalf("topology repeated or invented an input side: %q", queue.Side)
		}
		if queue.ID != "ingress/"+queue.Side || seenIDs[queue.ID] || queue.Label == "" {
			t.Fatalf("input queue lacks a unique identity or label: %+v", queue)
		}
		if queue.Capacity != capacity || queue.Stats != topologyStatsForTest(want) {
			t.Fatalf("topology queue %q lost its capacity or counters: %+v", queue.Side, queue)
		}
		seenSides[queue.Side] = true
		seenIDs[queue.ID] = true
	}
	for _, queue := range report.IngressQueues {
		want, exists := wantQueues[queue.Side]
		if !exists || queue.Capacity != capacity || queue.Stats != want {
			t.Fatalf("report queue lost its own capacity or counters: %+v", queue)
		}
	}
	sharedRate := s.ingress.Config().BandwidthBitsPerSecond
	if graph.Ingress.Config.BandwidthBitsPerSecond != sharedRate {
		t.Fatal("topology multiplied the shared ingress rate")
	}

	// Graph arrows show endpoint access sections, without an invented shared hop.
	if len(graph.Nodes) != 1+len(s.clients)+len(s.lb.GetServers()) {
		t.Fatal("topology added a synthetic network node")
	}
	if len(graph.Links) != 2*(len(s.clients)+len(s.lb.GetServers())) {
		t.Fatal("topology omitted a private path or added a fake ingress arrow")
	}
	for _, link := range graph.Links {
		if link.Source != loadBalancerNodeID && link.Target != loadBalancerNodeID {
			t.Fatalf("access or output path is not attached to the LB: %+v", link)
		}
	}

	// Editing a returned snapshot cannot change a later snapshot or the model.
	graph.Ingress.Config.BandwidthBitsPerSecond = 0
	graph.Ingress.Stats.QueuedPackets = -1
	graph.Ingress.Queues[0].Stats.QueuedPackets = -1
	report.IngressQueues[0].Stats.QueuedPackets = -1
	fresh := s.topology(report.State)
	if fresh.Ingress.Config.BandwidthBitsPerSecond != sharedRate {
		t.Fatal("ingress configuration escaped as a live reference")
	}
	if fresh.Ingress.Stats != topologyStatsForTest(aggregate) {
		t.Fatal("shared ingress counters escaped as a live reference")
	}
	for _, queue := range fresh.Ingress.Queues {
		if queue.Stats != topologyStatsForTest(wantQueues[queue.Side]) {
			t.Fatal("input queue counters escaped as a live reference")
		}
	}
	for _, queue := range s.result(0).IngressQueues {
		if queue.Stats != wantQueues[queue.Side] {
			t.Fatal("report queue counters escaped as a live reference")
		}
	}
}

// TestIngressLossPreventsLBReception distinguishes ingress loss from access loss.
func TestIngressLossPreventsLBReception(t *testing.T) {
	cfg := ingressTestConfiguration()
	cfg.ingress.LossRate = 1
	s := newSimulation(cfg)
	if err := s.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Access succeeds, but the common link loses the SYN after consuming bandwidth.
	if ingressClientAccessDeliveries(s) != 1 {
		t.Fatal("SYN did not reach the common ingress")
	}
	input := s.result(0).Ingress
	if input.LostPackets != 1 || input.LostBytes != 40 {
		t.Fatalf("ingress did not account for the lost wire packet: %+v", input)
	}
	if input.DeliveredPackets != 0 || input.AcceptedPackets != 1 {
		t.Fatalf("unexpected ingress admission or delivery: %+v", input)
	}
	clientLosses := s.clientIngress.Stats().LostPackets
	backendLosses := s.backendIngress.Stats().LostPackets
	if clientLosses != 1 || backendLosses != 0 {
		t.Fatal("shared receiver loss was attributed to the wrong input side")
	}
	srv := s.lb.GetServers()[0]
	if len(s.lb.GetConnectionTable()[srv]) != 0 || srv.NumberOfHandledPackets != 0 {
		t.Fatal("a lost ingress packet reached routing or the backend")
	}
	if s.egress.Stats().ForwardedPackets != 0 {
		t.Fatal("ingress loss still consumed an outgoing LB grant")
	}
}

// replaceIngressClient reuses a flow key while retaining a distinct client instance.
func replaceIngressClient(t *testing.T, s *simulation, old *client.Client) *client.Client {
	t.Helper()
	key := old.Key()
	output := firstIngressClientOutput(t, s)
	old.Shutdown()
	replacement := client.NewClient(
		s.engine,
		key.SrcIP,
		key.SrcPort,
		key.DstIP,
		key.DstPort,
		key.Protocol,
	)
	if err := s.lb.AddClient(replacement, func(pkt model.Packet) {
		output.Transmit(pkt.WireSize(), func() {
			replacement.HandlePacket(pkt)
		})
	}); err != nil {
		t.Fatal(err)
	}
	return replacement
}

// TestIngressPreservesClientIdentityAcrossBothHops rejects old packets after key reuse.
func TestIngressPreservesClientIdentityAcrossBothHops(t *testing.T) {
	tests := []struct {
		name        string
		fromBackend bool
		closeAt     time.Duration
		observeAt   time.Duration
	}{
		{
			name:      "client closes during access",
			closeAt:   time.Millisecond,
			observeAt: 130 * time.Millisecond,
		},
		{
			name:      "client closes during ingress",
			closeAt:   44 * time.Millisecond,
			observeAt: 130 * time.Millisecond,
		},
		{
			name:        "backend reply during access",
			fromBackend: true,
			closeAt:     174 * time.Millisecond,
			observeAt:   303 * time.Millisecond,
		},
		{
			name:        "backend reply during ingress",
			fromBackend: true,
			closeAt:     217 * time.Millisecond,
			observeAt:   303 * time.Millisecond,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newSimulation(ingressTestConfiguration())
			runIngressUntil(t, s, test.closeAt)
			old := firstIngressClient(t, s)
			replacement := replaceIngressClient(t, s, old)

			// Give a replacement the same route, so the pointer guard is essential.
			wantDeliveries := uint64(1)
			wantForwarded := uint64(0)
			if test.fromBackend {
				if _, err := s.lb.Route(replacement); err != nil {
					t.Fatal(err)
				}
				wantDeliveries = 2
				wantForwarded = 1
			}
			runIngressUntil(t, s, test.observeAt)

			// Old packets still complete both physical sections without reaching handlers.
			input := s.ingress.Stats()
			if input.DeliveredPackets != wantDeliveries || input.DeliveredBytes != wantDeliveries*40 {
				t.Fatalf("stale packet disappeared from physical ingress counters: %+v", input)
			}
			if s.egress.Stats().ForwardedPackets != wantForwarded {
				t.Fatal("old packet was forwarded on behalf of the replacement client")
			}
			if firstIngressClientOutput(t, s).Stats().OfferedPackets != 0 {
				t.Fatal("stale backend reply reached the replacement client's output")
			}
			if s.lb.HasClient(old) || !s.lb.HasClient(replacement) {
				t.Fatal("flow key reuse lost the replacement's instance identity")
			}
		})
	}
}

// TestIngressFrozenWorkAndFreshRun checks all unfinished link states together.
func TestIngressFrozenWorkAndFreshRun(t *testing.T) {
	cfg := ingressTestConfiguration()
	cfg.clients = 3
	cfg.bandwidth = 320_000
	cfg.packetTravelTime = 0
	cfg.duration = 45 * time.Millisecond
	cfg.ingress = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       10 * time.Millisecond,
		QueueCapacity:          2,
	}
	s := newSimulation(cfg)
	if err := s.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// At 45 ms, packet 1 travels, packet 2 transmits and packet 3 waits.
	first := s.result(0)
	want := network.Stats{
		OfferedPackets:    3,
		OfferedBytes:      120,
		AcceptedPackets:   3,
		AcceptedBytes:     120,
		TransmittedBytes:  40,
		QueuedPackets:     1,
		Transmitting:      true,
		TravellingPackets: 1,
	}
	if first.Ingress.Stats != want {
		t.Fatalf("frozen ingress = %+v, want %+v", first.Ingress.Stats, want)
	}
	if first.Forwarding.ForwardedPackets != 0 {
		t.Fatal("unfinished ingress packets reached LB forwarding")
	}
	assertIngressSnapshots(t, s)

	// A fresh simulation owns new queues, links and counters before it starts.
	fresh := newSimulation(cfg)
	if fresh.ingress == s.ingress || fresh.engine == s.engine {
		t.Fatal("a fresh simulation reused the old model graph")
	}
	reusedClientInput := fresh.clientIngress == s.clientIngress
	reusedBackendInput := fresh.backendIngress == s.backendIngress
	if reusedClientInput || reusedBackendInput {
		t.Fatal("a fresh simulation reused an old input queue")
	}
	if fresh.result(0).Ingress.Stats != (network.Stats{}) {
		t.Fatal("fresh ingress retained old counters or unfinished packets")
	}
	assertIngressSnapshots(t, fresh)
	if err := fresh.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, fresh.result(0)) {
		t.Fatal("fresh simulation did not reproduce the same virtual result")
	}
}
