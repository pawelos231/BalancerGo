package main

import (
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
			cfg.queue = test.capacity
			cfg.duration = 4 * time.Millisecond
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

			// Admitted packets finish by 4 ms; rejected callbacks never run.
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
