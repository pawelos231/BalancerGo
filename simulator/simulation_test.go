package main

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/network"
)

// TestSimulationQueueSnapshots checks reports and topology with both queue owners.
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
			cfg := legacyTestConfiguration()
			cfg.clients = 0
			cfg.bandwidth = 8_000
			cfg.forwardingBitsPerSecond = math.MaxInt64
			cfg.queue = test.capacity
			cfg.duration = 5 * time.Millisecond
			s := newSimulation(cfg)

			// Every path gets one active packet, then waiting packets or tail drops.
			arrivals := 0
			for _, link := range s.links {
				transmit := link.Link.Transmit
				if link.Source == "lb" {
					if link.Output == nil {
						t.Fatalf("LB output has no managed FIFO: %s", link.Name)
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
					t.Fatalf("LB output has a second hidden queue: %s", link.Name)
				}
			}

			// All admitted packets finish; rejected callbacks never run.
			if err := s.engine.RunUntil(cfg.duration); err != nil {
				t.Fatal(err)
			}
			finished := waiting
			finished.QueuedPackets = 0
			finished.Transmitting = false
			finished.DeliveredPackets = acceptedPackets
			finished.DeliveredBytes = test.acceptedBytes
			assertSimulationLinkSnapshots(t, s, finished)
			if arrivals != len(s.links)*int(acceptedPackets) {
				t.Fatalf("arrivals = %d, want %d", arrivals, len(s.links)*int(acceptedPackets))
			}

			// A new run starts with empty physical links and empty managed FIFOs.
			fresh := newSimulation(cfg)
			assertSimulationLinkSnapshots(t, fresh, network.Stats{})
		})
	}
}

// TestSimulationSharesForwardingBudget checks both LB directions and both snapshots.
func TestSimulationSharesForwardingBudget(t *testing.T) {
	cfg := legacyTestConfiguration()
	cfg.clients = 0
	cfg.servers = 1
	cfg.bandwidth = 8_000
	cfg.forwardingBitsPerSecond = 8_000
	cfg.queue = 1
	cfg.duration = 4 * time.Millisecond
	s := newSimulation(cfg)

	// Both incoming paths transmit independently of the LB's shared budget.
	arrivals := make(map[string]time.Duration)
	for _, link := range s.links {
		transmit := link.Link.Transmit
		if link.Output != nil {
			transmit = link.Output.Transmit
		}
		if !transmit(1, func() { arrivals[link.Name] = s.engine.Now() }) {
			t.Fatalf("first packet was rejected on %s", link.Name)
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

	if err := s.engine.RunUntil(cfg.duration); err != nil {
		t.Fatal(err)
	}
	wantArrivals := map[string]time.Duration{
		"clients -> LB": 2 * time.Millisecond,
		"LB -> clients": 2 * time.Millisecond,
		"LB -> srv-00":  3 * time.Millisecond,
		"srv-00 -> LB":  2 * time.Millisecond,
	}
	if !reflect.DeepEqual(arrivals, wantArrivals) {
		t.Fatalf("arrivals = %v, want %v", arrivals, wantArrivals)
	}
	assertForwardingSnapshots(t, s, started)

	// A new simulation owns a fresh pacer and fresh counters.
	fresh := newSimulation(cfg)
	assertForwardingSnapshots(t, fresh, network.ForwardingStats{
		ForwardingBitsPerSecond: cfg.forwardingBitsPerSecond,
	})
}

// TestSimulationCountsTCPWireTraffic checks that real model packets use the budget.
func TestSimulationCountsTCPWireTraffic(t *testing.T) {
	cfg := legacyTestConfiguration()
	cfg.clients = 2
	cfg.servers = 1
	cfg.streamBytes = 1024
	cfg.forwardingBitsPerSecond = 1_000_000

	var first simulationResult
	for run := 0; run < 2; run++ {
		s := newSimulation(cfg)
		if err := s.run(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		report := s.result(0)
		if report.CompletedStreams != cfg.clients || report.ClosedClients != cfg.clients {
			t.Fatalf("TCP streams did not finish: completed=%d closed=%d", report.CompletedStreams, report.ClosedClients)
		}

		// Count physical starts only on paths leaving the LB, including headers.
		var startedPackets, startedBytes uint64
		for _, link := range s.links {
			if link.Source == "lb" {
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

// assertForwardingSnapshots checks shared counters in the report and LB inspector.
func assertForwardingSnapshots(t *testing.T, s *simulation, want network.ForwardingStats) {
	t.Helper()
	report := s.result(0)
	if report.Forwarding != want {
		t.Fatalf("forwarding = %+v, want %+v", report.Forwarding, want)
	}

	graph := s.topology(report.State)
	metrics := make(map[string]float64)
	for _, node := range graph.Nodes {
		if node.ID == "lb" {
			for _, metric := range node.Metrics {
				metrics[metric.Key] = metric.Value
			}
		}
	}
	wantMetrics := map[string]float64{
		"forwardingBitsPerSecond":       float64(want.ForwardingBitsPerSecond),
		"forwardedPackets":              float64(want.ForwardedPackets),
		"forwardedBytes":                float64(want.ForwardedBytes),
		"forwardingQueuedPackets":       float64(want.QueuedPackets),
		"forwardingQueueDroppedPackets": float64(want.QueueDroppedPackets),
		"forwardingQueueDroppedBytes":   float64(want.QueueDroppedBytes),
		"forwardingBudgetWaitMs":        float64(want.BudgetWait) / float64(time.Millisecond),
	}
	for key, value := range wantMetrics {
		got, exists := metrics[key]
		if !exists || got != value {
			t.Errorf("topology metric %s = %v (exists=%v), want %v", key, got, exists, value)
		}
	}
}

// assertSimulationLinkSnapshots checks both public projections of link counters.
func assertSimulationLinkSnapshots(t *testing.T, s *simulation, want network.Stats) {
	t.Helper()
	report := s.result(0)
	graph := s.topology(report.State)
	if len(report.Links) != len(s.links) || len(graph.Links) != len(s.links) {
		t.Fatal("report or topology omitted a wired link")
	}

	wantGraphStats := model.TopologyLinkStats{
		QueuedPackets:       want.QueuedPackets,
		Transmitting:        want.Transmitting,
		TravellingPackets:   want.TravellingPackets,
		OfferedPackets:      want.OfferedPackets,
		DeliveredPackets:    want.DeliveredPackets,
		QueueDroppedPackets: want.QueueDroppedPackets,
		LostPackets:         want.LostPackets,
		DeliveredBytes:      want.DeliveredBytes,
	}
	for i, link := range s.links {
		if report.Links[i].Name != link.Name || report.Links[i].Stats != want {
			t.Errorf("report for %s: got %+v, want %+v", link.Name, report.Links[i], want)
		}
		if graph.Links[i].ID != link.ID || graph.Links[i].Stats != wantGraphStats {
			t.Errorf("topology for %s: got %+v, want %+v", link.Name, graph.Links[i], wantGraphStats)
		}
	}
}
