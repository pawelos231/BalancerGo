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
	key := model.ClientKey{
		SrcIP:    "192.0.0.1",
		SrcPort:  40001,
		DstIP:    "lb.local",
		DstPort:  80,
		Protocol: model.TCP,
	}
	cl := s.lb.GetClient(key)
	if cl == nil {
		t.Fatal("production client has not been created")
	}
	return cl
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

	// The backend receives the SYN at 173 ms; its actual SYN-ACK reaches upstream at 216 ms.
	runIngressUntil(t, s, 216*time.Millisecond)
	if srv.NumberOfHandledPackets != 1 {
		t.Fatal("backend did not receive the forwarded SYN")
	}
	input = s.ingress.Stats()
	if input.OfferedPackets != 2 || input.OfferedBytes != 80 {
		t.Fatalf("backend reply did not join the same physical ingress: %+v", input)
	}
	if s.downlink.Stats().OfferedPackets != 0 {
		t.Fatal("backend reply bypassed ingress on its way to the client")
	}

	// The reply pays the same ingress cost before the LB can send it onward.
	runIngressUntil(t, s, 303*time.Millisecond-time.Nanosecond)
	if s.downlink.Stats().OfferedPackets != 0 {
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
	if firstIngressClient(t, s).IsHandshakeCompleted() {
		t.Fatal("client received SYN-ACK before its outgoing link completed")
	}
}

// TestIngressClientBurstUsesOneFiniteFIFO checks real clients competing upstream.
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

			// Three SYNs leave access at 1, 2 and 3 ms; ingress needs 40 ms each.
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
			if s.uplink.Stats().DeliveredPackets != 3 {
				t.Fatal("client SYNs were dropped before reaching ingress")
			}
			if s.egress.Stats().ForwardedPackets != 0 {
				t.Fatal("waiting or dropped SYNs reached an LB packet handler")
			}
		})
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
	if s.uplink.Stats().DeliveredPackets != 1 {
		t.Fatal("SYN did not reach the common ingress")
	}
	input := s.result(0).Ingress
	if input.LostPackets != 1 || input.LostBytes != 40 {
		t.Fatalf("ingress did not account for the lost wire packet: %+v", input)
	}
	if input.DeliveredPackets != 0 || input.AcceptedPackets != 1 {
		t.Fatalf("unexpected ingress admission or delivery: %+v", input)
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
		s.downlink.Transmit(pkt.WireSize(), func() {
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
			if s.downlink.Stats().OfferedPackets != 0 {
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

	// A fresh simulation owns new queues, links and counters before it starts.
	fresh := newSimulation(cfg)
	if fresh.ingress == s.ingress || fresh.engine == s.engine {
		t.Fatal("a fresh simulation reused the old model graph")
	}
	if fresh.result(0).Ingress.Stats != (network.Stats{}) {
		t.Fatal("fresh ingress retained old counters or unfinished packets")
	}
	if err := fresh.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, fresh.result(0)) {
		t.Fatal("fresh simulation did not reproduce the same virtual result")
	}
}
