package presentation

import (
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

// Link edits, reduced budgets and later removal keep born clients in every graph.
func TestTimelineClientChangesPublishCoherentRetainedDefinitions(t *testing.T) {
	input := timelineProjectionDefinition()
	initial := input.States["0"]
	next := input.States["250000000"]
	next.Workload.ClientGroups = append(
		[]scenario.ClientGroup(nil), initial.Workload.ClientGroups...,
	)
	group := &next.Workload.ClientGroups[0]
	group.Count = 1
	bytes, rate := 8192, int64(5_000_000)
	group.Stream.Bytes = &bytes
	group.Network.ToLoadBalancer.BandwidthBitsPerSecond = &rate
	group.Network.FromLoadBalancer.BandwidthBitsPerSecond = &rate
	input.States["250000000"] = next
	removed := next
	removed.Workload.ClientGroups = []scenario.ClientGroup{{
		ID: "new", Count: 1, StartAt: 375 * time.Millisecond,
	}}
	input.States["375000000"] = removed
	world := timelineProjectionFrom(t, input)
	advanceProjection(t, world, 0)
	projector := NewProjector("clients")
	first := project(t, projector, world)
	oldDocument := jsonProjection(t, first.Document)

	advanceProjection(t, world, 250*time.Millisecond)
	frame, document, err := BuildSnapshot(world, projector, RunState{Status: "running"})
	if err != nil || document == nil || document.Revision != "2" ||
		frame.Topology.Revision != "2" || world.Info().ScheduledClients != 2 {
		t.Fatalf("client edits did not publish one complete revision: %v", err)
	}
	seenClients, seenLinks := 0, 0
	for _, node := range document.Nodes {
		if node.Kind != contract.TopologyNodeClient {
			continue
		}
		seenClients++
		for _, metric := range node.Metrics {
			if metric.Key == "payloadBytes" && (metric.Value == nil || *metric.Value != 4096) {
				t.Fatal("the definition replaced an existing client's birth stream")
			}
		}
		for _, link := range document.Links {
			if link.Source == node.ID || link.Target == node.ID {
				seenLinks++
				if link.Config.BandwidthBitsPerSecond != rate {
					t.Fatal("the refreshed graph retained an old client link limit")
				}
			}
		}
	}
	if seenClients != 2 || seenLinks != 4 {
		t.Fatal("the reduced budget removed born clients or their links")
	}

	advanceProjection(t, world, 375*time.Millisecond)
	last := project(t, projector, world)
	if last.Document == nil || last.Topology.Revision != "3" ||
		len(last.Document.Nodes) != len(first.Document.Nodes)+1 ||
		len(last.Document.Links) != len(first.Document.Links)+2 {
		t.Fatal("removing a generator lost its clients while adding a new group")
	}
	if jsonProjection(t, first.Document) != oldDocument || project(t, projector, world).Document != nil {
		t.Fatal("client changes mutated an old document or invalidated the current cache")
	}
}
