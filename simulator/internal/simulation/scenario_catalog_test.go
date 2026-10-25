package simulation

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
)

// catalogScenario shortens detached examples while keeping their network and arrival settings.
func catalogScenario(t *testing.T, id string, duration time.Duration, payload int) scenario.Scenario {
	t.Helper()
	path := filepath.Join("..", "..", "..", "examples", "scenarios", id+".json")
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	input, err := scenario.DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	input.Execution.Duration = duration
	input.Execution.SnapshotInterval = 50 * time.Millisecond
	input.Execution.Speed = 0
	input.Execution.MaxEvents = 1_000_000
	input.Workload.Defaults.Bytes = payload
	for index := range input.Workload.ClientGroups {
		group := &input.Workload.ClientGroups[index]
		if group.Stream.Bytes != nil && *group.Stream.Bytes > payload {
			*group.Stream.Bytes = payload
		}
		for position := range group.Overrides {
			stream := &group.Overrides[position].Stream
			if stream.Bytes != nil && *stream.Bytes > payload {
				*stream.Bytes = payload
			}
		}
	}
	return input
}

// runCatalogScenario retains the model so tests can inspect individual physical paths.
func runCatalogScenario(t *testing.T, input scenario.Scenario) *World {
	t.Helper()
	s := New(normalizedRuntimeScenario(t, input))
	if err := runWorld(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.engine.Now() != input.Execution.Duration {
		t.Fatal("catalog run did not reach its virtual horizon")
	}
	return s
}

// catalogClientTraffic counts original uplinks once, without their second ingress hop.
func catalogClientTraffic(t *testing.T, s *World) (offered, lost uint64) {
	t.Helper()
	for _, cl := range s.clients {
		stats := scenarioRuntimeLink(t, s, cl.ID, LoadBalancerNodeID).Stats()
		offered += stats.OfferedPackets
		lost += stats.LostPackets
	}
	return offered, lost
}

// TestCatalogIngressBurst checks admission before the first SYN finishes shared serialization.
func TestCatalogIngressBurst(t *testing.T) {
	cases := []struct {
		id       string
		capacity int
	}{
		{id: "tiny-queues", capacity: 2},
		{id: "unbuffered-ingress", capacity: 0},
	}
	for _, test := range cases {
		t.Run(test.id, func(t *testing.T) {
			input := catalogScenario(t, test.id, 50*time.Millisecond, 2050)
			resolved := normalizedRuntimeScenario(t, input)
			if resolved.LoadBalancer.Ingress.QueueCapacity != test.capacity {
				t.Fatal("the example no longer exercises its advertised waiting capacity")
			}
			group := resolved.ClientGroups[0]
			if len(resolved.ClientGroups) != 1 || group.StartAt != 0 || group.ClientGap != 0 || group.Count <= test.capacity+1 {
				t.Fatal("the queue example must offer a simultaneous burst larger than its capacity")
			}

			// All private access links deliver their first 40-byte SYN at this timestamp.
			access := group.Network.ToLoadBalancer
			bitsNanoseconds := int64(40 * 8 * time.Second)
			serialization := bitsNanoseconds / access.BandwidthBitsPerSecond
			if bitsNanoseconds%access.BandwidthBitsPerSecond != 0 {
				serialization++
			}
			arrival := time.Duration(serialization) + access.PacketTravelTime
			var first network.Stats
			for run := 0; run < 2; run++ {
				s := New(resolved)
				if s.ingress.Stats() != (network.Stats{}) {
					t.Fatal("a fresh model retained the previous burst")
				}
				if err := s.engine.RunUntil(arrival); err != nil {
					t.Fatal(err)
				}
				stats := s.clientIngress.Stats()
				accepted := uint64(test.capacity + 1)
				if stats.OfferedPackets != uint64(group.Count) || stats.AcceptedPackets != accepted {
					t.Fatalf("burst admissions = %+v", stats)
				}
				if stats.QueueDroppedPackets != uint64(group.Count)-accepted || stats.QueuedPackets != test.capacity || !stats.Transmitting {
					t.Fatalf("the waiting FIFO did not tail-drop the excess SYNs: %+v", stats)
				}
				if s.egress.Stats().ForwardedPackets != 0 || s.backendIngress.Stats().OfferedPackets != 0 {
					t.Fatal("the first SYN bypassed shared ingress serialization")
				}
				if run == 0 {
					first = stats
				} else if stats != first {
					t.Fatal("a fresh model did not reproduce the same burst and drops")
				}
			}
		})
	}
}

// TestCatalogSlowACKPath compares the return-link bottleneck with an otherwise identical run.
func TestCatalogSlowACKPath(t *testing.T) {
	input := catalogScenario(t, "slow-ack-path", time.Second, 1<<20)
	slow := runCatalogScenario(t, input)
	control := scenarioFileRoundTrip(t, input)
	var slowData uint64
	for index, backend := range slow.scenario.Backends {
		returnLink := scenarioRuntimeLink(t, slow, BackendNodeID(backend.ID), LoadBalancerNodeID)
		stats := returnLink.Stats()
		config := returnLink.Link.Config()
		if config.BandwidthBitsPerSecond != 64_000 || backend.Network.FromLoadBalancer.BandwidthBitsPerSecond != 1_000_000_000 {
			t.Fatal("the slow ACK example must constrain backend-to-LB traffic only")
		}
		// Completed serialization cannot exceed one virtual second of this link's rate.
		byteLimit := uint64(config.BandwidthBitsPerSecond / 8)
		if stats.TransmittedBytes == 0 || stats.TransmittedBytes > byteLimit || stats.QueuedPackets == 0 {
			t.Fatalf("return traffic did not exercise its physical bottleneck: %+v", stats)
		}
		dataLink := scenarioRuntimeLink(t, slow, LoadBalancerNodeID, BackendNodeID(backend.ID))
		slowData += dataLink.Stats().DeliveredBytes
		control.Resources.Backends[index].Network.ToLoadBalancer.BandwidthBitsPerSecond = scenarioRuntimeValue[int64](1_000_000_000)
	}
	fast := runCatalogScenario(t, control)
	var fastData uint64
	for _, backend := range fast.scenario.Backends {
		fastData += scenarioRuntimeLink(t, fast, LoadBalancerNodeID, BackendNodeID(backend.ID)).Stats().DeliveredBytes
	}
	if slowData == 0 || fastData <= 2*slowData {
		t.Fatalf("removing only the ACK bottleneck did not increase data progress: slow=%d fast=%d", slowData, fastData)
	}
}

// TestCatalogLossyAccessReplays verifies actual loss and extra TCP attempts with a fixed seed.
func TestCatalogLossyAccessReplays(t *testing.T) {
	input := catalogScenario(t, "lossy-access", 7*time.Second, 2050)
	lossy := runCatalogScenario(t, input)
	offered, lost := catalogClientTraffic(t, lossy)
	if lost == 0 || lossy.completedStreams == 0 {
		t.Fatal("the lossy example did not exercise both packet loss and successful streams")
	}
	control := scenarioFileRoundTrip(t, input)
	for index := range control.Workload.ClientGroups {
		control.Workload.ClientGroups[index].Network.ToLoadBalancer.LossRate = scenarioRuntimeValue(0.0)
	}
	clean := runCatalogScenario(t, control)
	cleanOffered, cleanLost := catalogClientTraffic(t, clean)
	if cleanLost != 0 || offered <= cleanOffered {
		t.Fatalf("loss did not produce extra client attempts: lossy=%d clean=%d losses=%d", offered, cleanOffered, lost)
	}
	replay := runCatalogScenario(t, scenarioFileRoundTrip(t, input))
	if !reflect.DeepEqual(readWorld(lossy), readWorld(replay)) {
		t.Fatal("the same lossy file and seed did not reproduce virtual results")
	}
}

// TestCatalogBlackholedBackend keeps failure on one physical branch without moving its flows.
func TestCatalogBlackholedBackend(t *testing.T) {
	s := runCatalogScenario(t, catalogScenario(t, "blackholed-backend", 7*time.Second, 2050))
	bad := scenarioRuntimeLink(t, s, LoadBalancerNodeID, BackendNodeID("backend-1")).Stats()
	good := scenarioRuntimeLink(t, s, LoadBalancerNodeID, BackendNodeID("backend-2")).Stats()
	if bad.LostPackets == 0 || bad.DeliveredPackets != 0 || good.LostPackets != 0 || good.DeliveredPackets == 0 {
		t.Fatalf("the two backend paths did not have distinct physical outcomes: bad=%+v good=%+v", bad, good)
	}
	if s.completedStreams != s.createdClients/2 || s.completedPayloadBytes != uint64(s.completedStreams*2050) {
		t.Fatal("only clients assigned to the healthy round-robin branch should complete")
	}
	for _, srv := range s.lb.GetServers() {
		if srv.ID == "backend-1" && srv.NumberOfHandledPackets != 0 {
			t.Fatal("the backend handled packets discarded on its incoming link")
		}
	}
}

// TestCatalogArrivalWaves checks lazy creation at each wave and skips the wave beyond the horizon.
func TestCatalogArrivalWaves(t *testing.T) {
	input := catalogScenario(t, "staggered-arrivals", 180*time.Second, 2050)
	s := New(normalizedRuntimeScenario(t, input))
	if s.createdClients != 0 || s.scheduledClients != 16 {
		t.Fatal("future groups must reserve identities without constructing clients")
	}
	boundaries := []struct {
		at      time.Duration
		created int
	}{
		{at: 0, created: 1},
		{at: 5 * time.Second, created: 5},
		{at: 20 * time.Second, created: 9},
		{at: input.Execution.Duration, created: 12},
	}
	for _, boundary := range boundaries {
		if err := s.engine.RunUntil(boundary.at); err != nil {
			t.Fatal(err)
		}
		if s.createdClients != boundary.created {
			t.Fatalf("at %s created %d clients, want %d", boundary.at, s.createdClients, boundary.created)
		}
	}
	for _, cl := range s.clients {
		if cl.GroupID == "after-horizon" {
			t.Fatal("a client scheduled at 200 s was created before the 180 s horizon")
		}
	}
	if s.completedStreams != 12 || s.engine.Pending() != 0 {
		t.Fatal("the created short streams or their scheduled callbacks did not finish")
	}
}

// TestCatalogMixedStreams checks empty, full and partial segments against actual wire counters.
func TestCatalogMixedStreams(t *testing.T) {
	s := runCatalogScenario(t, catalogScenario(t, "mixed-streams", 7*time.Second, 1<<20))
	wantShortBytes := []int{0, 1, 1024, 1025}
	var expectedPayload uint64
	for _, cl := range s.clients {
		bytes := cl.Config.Stream.Bytes
		if cl.GroupID == "short-streams" && cl.Index < len(wantShortBytes) && bytes != wantShortBytes[cl.Index] {
			t.Fatalf("short client %d received %d payload bytes", cl.Index, bytes)
		}
		if !cl.Client.StreamCompleted() || !cl.Client.IsClosed() {
			t.Fatalf("bounded mixed stream %s did not complete and close", cl.ID)
		}
		expectedPayload += uint64(bytes)
		segments := (bytes + model.SEGMENT_SIZE - 1) / model.SEGMENT_SIZE
		// Uplink includes SYN, standalone handshake ACK, data and FIN.
		wantPackets := uint64(segments + 3)
		wantBytes := uint64(bytes) + 40*wantPackets
		stats := scenarioRuntimeLink(t, s, cl.ID, LoadBalancerNodeID).Stats()
		if stats.DeliveredPackets != wantPackets || stats.DeliveredBytes != wantBytes {
			t.Fatalf("client %s wire traffic = %+v, want %d packets / %d bytes", cl.ID, stats, wantPackets, wantBytes)
		}
	}
	if s.completedStreams != 16 || s.completedPayloadBytes != expectedPayload {
		t.Fatal("completed payload totals disagree with the resolved per-client streams")
	}
}

// TestCatalogEmptyCases reaches the horizon successfully without inventing traffic or completions.
func TestCatalogEmptyCases(t *testing.T) {
	for _, id := range []string{"idle-network", "no-backends"} {
		t.Run(id, func(t *testing.T) {
			s := runCatalogScenario(t, catalogScenario(t, id, 3*time.Second, 2050))
			if s.completedStreams != 0 || s.egress.Stats().ForwardedPackets != 0 {
				t.Fatal("an empty case invented forwarded packets or completed streams")
			}
			if id == "idle-network" {
				if s.createdClients != 0 || s.ingress.Stats().OfferedPackets != 0 {
					t.Fatal("the idle network created clients or ingress traffic")
				}
				return
			}
			if s.createdClients != 8 || len(s.lb.GetServers()) != 0 || s.clientIngress.Stats().DeliveredPackets == 0 {
				t.Fatal("the backend-free example did not deliver clients' SYN attempts")
			}
			for _, cl := range s.clients {
				if cl.Client.IsHandshakeCompleted() || cl.Client.GetState().RetrySyn == 0 {
					t.Fatal("a backend-free client established a connection or failed to retry SYN")
				}
			}
		})
	}
}
