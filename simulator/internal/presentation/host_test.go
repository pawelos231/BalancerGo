package presentation

import (
	"testing"
	"time"

	"load-balancer/scenario"
)

// Reports and topology expose the same virtual CPU facts; reading cannot consume work.
func TestHostCountersAreProjectedWithoutChangingTheWorld(t *testing.T) {
	definition := projectionScenario()
	cores, queue, cost := 1, 2, time.Millisecond
	definition.Resources.Backends[0].Host = scenario.HostConfig{
		Cores: &cores, PacketProcessingTime: &cost, QueueCapacity: &queue,
	}
	world := projectionWorld(t, definition)
	advanceProjection(t, world, world.EndTime())
	before := world.EventsExecuted()
	report := BuildResult(world, 0)
	state := report.State.Servers[0].Server.Host
	if state == nil || !state.Enabled || state.ProcessedPackets == 0 || state.Utilization <= 0 {
		t.Fatalf("host facts missing from report: %+v", state)
	}
	sample := project(t, NewProjector("host"), world)
	found := false
	for _, node := range sample.Topology.Nodes {
		if node.ID == "backend/one" {
			found = true
			if node.Metrics["hostProcessedPackets"] != float64(state.ProcessedPackets) ||
				node.Metrics["hostUtilization"] != state.Utilization {
				t.Fatalf("topology and report CPU facts differ: %+v", node.Metrics)
			}
		}
	}
	if !found || world.EventsExecuted() != before {
		t.Fatal("projecting CPU state lost a node or advanced the simulation")
	}
}
