package presentation

import (
	"testing"
	"time"

	"load-balancer/host"
	"load-balancer/scenario"
)

// Reports and topology expose the same virtual CPU facts; reading cannot consume work.
func TestHostCountersAreProjectedWithoutChangingTheWorld(t *testing.T) {
	definition := projectionScenario()
	cores, queue, cost := 1, 2, time.Millisecond
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.Backends[0].Host = scenario.HostConfig{
			Cores: &cores, PacketProcessingTime: &cost, QueueCapacity: &queue,
		}
	})
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

func TestHostProjectionRetainsCPUHistoryAfterBypass(t *testing.T) {
	state := host.Stats{
		Cores: 1, BusyCores: 2, ProcessedPackets: 3,
		BusyTime: 2 * time.Second, AvailableCoreTime: 3 * time.Second,
		ObservedTime: 2 * time.Second,
	}
	projected := hostState(state)
	if projected.Enabled || projected.AvailableCoreTimeSeconds != 3 ||
		projected.BusyTimeSeconds != 2 || projected.Utilization != 2.0/3.0 {
		t.Fatalf("disabled host lost earlier CPU work: %+v", projected)
	}

	metrics := appendHostMetrics(nil, state)
	values := make(map[string]float64, len(metrics))
	for _, metric := range metrics {
		values[metric.key] = numericMetric(t, metric.value)
	}
	if values["hostCPUEnabled"] != 0 || values["hostBusyCores"] != 2 ||
		values["hostUtilization"] != 2.0/3.0 || values["hostAvailableCoreTimeSeconds"] != 3 {
		t.Fatalf("topology lost retiring CPU work: %+v", values)
	}

	state.BusyCores = 0
	if len(appendHostMetrics(nil, state)) <= 1 {
		t.Fatal("completed CPU history disappeared after retiring jobs finished")
	}
}
