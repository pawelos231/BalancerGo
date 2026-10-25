package simulation

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/network"
)

// TestSimulationQueueSnapshots checks native observations with both queue owners.
func TestSimulationQueueSnapshots(t *testing.T) {
	cases := []struct {
		name          string
		capacity      int
		acceptedBytes uint64
	}{
		{name: "no waiting slots", capacity: 0, acceptedBytes: 1},
		{name: "one waiting slot", capacity: 1, acceptedBytes: 3},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			definition := baseScenario()
			definition.Workload.ClientGroups[0].Count = 0
			definition.Network.Defaults.BandwidthBitsPerSecond = 8_000
			definition.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = definition.Network.Defaults.BandwidthBitsPerSecond
			definition.Resources.LoadBalancer.Ingress.PacketTravelTime = definition.Network.Defaults.PacketTravelTime
			definition.Resources.LoadBalancer.Ingress.QueueCapacity = test.capacity
			definition.Resources.LoadBalancer.ForwardingBitsPerSecond = math.MaxInt64
			definition.Network.Defaults.QueueCapacity = test.capacity
			definition.Execution.Duration = 5 * time.Millisecond
			s := New(normalizedRuntimeScenario(t, definition))

			// Every path gets one active packet, then waiting packets or tail drops.
			arrivals := 0
			for _, link := range s.links {
				transmit := link.Link.Transmit
				if link.Source == LoadBalancerNodeID {
					if link.Output == nil {
						t.Fatalf("LB output has no managed FIFO: %s", link.ID)
					}
					transmit = link.Output.Transmit
				}
				for size := 1; size <= 3; size++ {
					transmit(size, func() { arrivals++ })
				}

				// Let the fast shared budget recover before offering the next path.
				if err := s.engine.RunUntil(s.engine.Now() + time.Nanosecond); err != nil {
					t.Fatal(err)
				}
			}

			// Snapshots must include the LB FIFO, even though its physical queue is empty.
			acceptedPackets := uint64(1 + test.capacity)
			waiting := network.Stats{
				OfferedPackets:      3,
				OfferedBytes:        6,
				AcceptedPackets:     acceptedPackets,
				AcceptedBytes:       test.acceptedBytes,
				QueueDroppedPackets: 3 - acceptedPackets,
				QueueDroppedBytes:   6 - test.acceptedBytes,
				QueuedPackets:       test.capacity,
				Transmitting:        true,
			}
			assertSimulationLinkSnapshots(t, s, waiting)
			for _, link := range s.links {
				if link.Output != nil && link.Link.Stats().QueuedPackets != 0 {
					t.Fatalf("LB output has a second hidden queue: %s", link.ID)
				}
			}

			// All admitted packets finish; rejected callbacks never run.
			if err := s.engine.RunUntil(definition.Execution.Duration); err != nil {
				t.Fatal(err)
			}
			finished := waiting
			finished.QueuedPackets = 0
			finished.Transmitting = false
			finished.DeliveredPackets = acceptedPackets
			finished.DeliveredBytes = test.acceptedBytes
			finished.TransmittedBytes = test.acceptedBytes
			assertSimulationLinkSnapshots(t, s, finished)
			if arrivals != len(s.links)*int(acceptedPackets) {
				t.Fatalf("arrivals = %d, want %d", arrivals, len(s.links)*int(acceptedPackets))
			}

			// A new run starts with empty physical links and empty managed FIFOs.
			fresh := New(normalizedRuntimeScenario(t, definition))
			assertSimulationLinkSnapshots(t, fresh, network.Stats{})
		})
	}
}

// TestSimulationSharesForwardingBudget checks shared outputs and native readings.
func TestSimulationSharesForwardingBudget(t *testing.T) {
	definition := baseScenario()
	definition.Workload.ClientGroups[0].Count = 0
	definition.Resources.Backends = definition.Resources.Backends[:2]
	definition.Network.Defaults.BandwidthBitsPerSecond = 8_000
	definition.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = definition.Network.Defaults.BandwidthBitsPerSecond
	definition.Resources.LoadBalancer.Ingress.PacketTravelTime = definition.Network.Defaults.PacketTravelTime
	definition.Resources.LoadBalancer.ForwardingBitsPerSecond = 8_000
	definition.Network.Defaults.QueueCapacity = 1
	definition.Execution.Duration = 4 * time.Millisecond
	s := New(normalizedRuntimeScenario(t, definition))

	// Physical input links do not consume the LB's output forwarding budget.
	arrivals := make(map[string]time.Duration)
	for _, link := range s.links {
		transmit := link.Link.Transmit
		if link.Output != nil {
			transmit = link.Output.Transmit
		}
		path := link.Source + " -> " + link.Target
		if !transmit(1, func() { arrivals[path] = s.engine.Now() }) {
			t.Fatalf("first packet was rejected on %s", link.ID)
		}
	}

	// The first LB output starts at zero; the second waits for the same budget.
	if err := s.engine.RunUntil(500 * time.Microsecond); err != nil {
		t.Fatal(err)
	}
	waiting := network.ForwardingStats{
		ForwardingBitsPerSecond: 8_000,
		ForwardedPackets:        1,
		ForwardedBytes:          1,
		QueuedPackets:           1,
		BudgetWait:              500 * time.Microsecond,
		BudgetBlocked:           true,
	}
	assertForwardingSnapshots(t, s, waiting)
	assertForwardingSnapshots(t, s, waiting)

	// The next grant starts the backend output at 1 ms, with no extra network hop.
	if err := s.engine.RunUntil(1500 * time.Microsecond); err != nil {
		t.Fatal(err)
	}
	started := network.ForwardingStats{
		ForwardingBitsPerSecond: 8_000,
		ForwardedPackets:        2,
		ForwardedBytes:          2,
		BudgetWait:              time.Millisecond,
	}
	assertForwardingSnapshots(t, s, started)

	if err := s.engine.RunUntil(definition.Execution.Duration); err != nil {
		t.Fatal(err)
	}
	wantArrivals := map[string]time.Duration{
		LoadBalancerNodeID + " -> " + BackendNodeID("srv-00"): 2 * time.Millisecond,
		LoadBalancerNodeID + " -> " + BackendNodeID("srv-01"): 3 * time.Millisecond,
		BackendNodeID("srv-00") + " -> " + LoadBalancerNodeID: 2 * time.Millisecond,
		BackendNodeID("srv-01") + " -> " + LoadBalancerNodeID: 2 * time.Millisecond,
	}
	if !reflect.DeepEqual(arrivals, wantArrivals) {
		t.Fatalf("arrivals = %v, want %v", arrivals, wantArrivals)
	}
	assertForwardingSnapshots(t, s, started)

	// A new simulation owns a fresh pacer and fresh counters.
	fresh := New(normalizedRuntimeScenario(t, definition))
	assertForwardingSnapshots(t, fresh, network.ForwardingStats{
		ForwardingBitsPerSecond: definition.Resources.LoadBalancer.ForwardingBitsPerSecond,
	})
}

// assertForwardingSnapshots verifies a detached reading without consuming budget
// or changing its virtual-time wait measurement.
func assertForwardingSnapshots(t *testing.T, s *World, want network.ForwardingStats) {
	t.Helper()
	if got := s.Info().Forwarding; got != want {
		t.Fatalf("forwarding = %+v, want %+v", got, want)
	}
}

// assertSimulationLinkSnapshots checks every physical and managed queue through
// the same detached observations used by the presentation boundary.
func assertSimulationLinkSnapshots(t *testing.T, s *World, want network.Stats) {
	t.Helper()
	index := 0
	s.VisitLinks(func(link LinkObservation) {
		if index >= len(s.links) {
			t.Fatal("observation introduced an unwired link")
		}
		wired := s.links[index]
		if link.ID != wired.ID || link.Stats != want {
			t.Errorf("observation for %s: got %+v, want %+v", wired.ID, link, want)
		}
		index++
	})
	if index != len(s.links) {
		t.Fatal("observation omitted a wired link")
	}
}

// TestSimulationCountsTCPWireTraffic checks that real model packets use the budget.
func TestSimulationCountsTCPWireTraffic(t *testing.T) {
	definition := baseScenario()
	definition.Workload.ClientGroups[0].Count = 2
	definition.Resources.Backends = definition.Resources.Backends[:1]
	definition.Workload.Defaults.Bytes = 1024
	definition.Resources.LoadBalancer.ForwardingBitsPerSecond = 1_000_000

	var first worldReadings
	for run := 0; run < 2; run++ {
		s := New(normalizedRuntimeScenario(t, definition))
		if err := runWorld(context.Background(), s); err != nil {
			t.Fatal(err)
		}
		report := readWorld(s)
		if report.CompletedStreams != definition.Workload.ClientGroups[0].Count || report.ClosedClients != definition.Workload.ClientGroups[0].Count {
			t.Fatalf("TCP streams did not finish: completed=%d closed=%d", report.CompletedStreams, report.ClosedClients)
		}

		// Count physical starts only on paths leaving the LB, including headers.
		var startedPackets, startedBytes uint64
		for _, link := range s.links {
			if link.Source == LoadBalancerNodeID {
				physical := link.Link.Stats()
				startedPackets += physical.AcceptedPackets
				startedBytes += physical.AcceptedBytes
			}
		}
		if report.Forwarding.ForwardedPackets != startedPackets {
			t.Fatalf("forwarded packets = %d, physical LB starts = %d", report.Forwarding.ForwardedPackets, startedPackets)
		}
		if report.Forwarding.ForwardedBytes != startedBytes {
			t.Fatalf("forwarded bytes = %d, physical LB bytes = %d", report.Forwarding.ForwardedBytes, startedBytes)
		}
		if startedBytes <= report.CompletedPayloadBytes {
			t.Fatal("forwarding omitted TCP headers or control packets")
		}

		// Equal seeds and scenarios retain the same virtual result.
		if run == 0 {
			first = report
		} else if !reflect.DeepEqual(first, report) {
			t.Fatal("identical scenarios produced different virtual results")
		}
	}
}
