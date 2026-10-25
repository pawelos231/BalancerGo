package presentation

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

func projectionScenario() scenario.Scenario {
	link := scenario.LinkConfig{BandwidthBitsPerSecond: 8_000_000, QueueCapacity: 32}
	return scenario.Scenario{
		Version:   scenario.Version,
		Execution: scenario.ExecutionConfig{Duration: 500 * time.Millisecond, SnapshotInterval: time.Millisecond, Seed: 41, MaxEvents: 1_000_000},
		Network:   scenario.NetworkConfig{Defaults: link},
		Resources: scenario.ResourcesConfig{
			LoadBalancer: scenario.LoadBalancerConfig{ID: "lb", Algorithm: "roundRobin", Ingress: link, ForwardingBitsPerSecond: math.MaxInt64},
			Backends:     []scenario.BackendConfig{{ID: "one", Address: "10.0.0.1", Port: 80}},
		},
		Workload: scenario.WorkloadConfig{
			Defaults:     scenario.StreamConfig{Bytes: 4096, PacketGap: time.Second},
			ClientGroups: []scenario.ClientGroup{{ID: "clients", Count: 2}},
		},
	}
}

func projectionWorld(t *testing.T, definition scenario.Scenario) *simulation.World {
	t.Helper()
	normalized, err := scenario.Normalize(definition)
	if err != nil {
		t.Fatal(err)
	}
	return simulation.New(normalized)
}

func advanceProjection(t *testing.T, world *simulation.World, until time.Duration) {
	t.Helper()
	if err := world.Advance(context.Background(), until); err != nil {
		t.Fatal(err)
	}
	world.SampleTraffic()
}

func project(t *testing.T, projector *Projector, world *simulation.World) Sample {
	t.Helper()
	sample, err := projector.Sample(world)
	if err != nil {
		t.Fatal(err)
	}
	return sample
}

func jsonProjection(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// The deliberately slower first client distinguishes creation order from the
// connection-table order. Repeated observations must leave the world untouched.
func TestProjectionKeepsCreationAndRoutingOrders(t *testing.T) {
	definition := projectionScenario()
	delay := time.Millisecond
	definition.Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{{
		Index: 0, Network: scenario.EndpointNetworkConfig{ToLoadBalancer: scenario.LinkPatch{PacketTravelTime: &delay}},
	}}
	world := projectionWorld(t, definition)
	advanceProjection(t, world, 0)
	projector := NewProjector("ordered")
	first := project(t, projector, world)
	if first.Document.RunID != "ordered" || first.Document.Revision != "1" {
		t.Fatal("first definitions did not establish the run revision")
	}
	frozen := jsonProjection(t, first.Document)
	advanceProjection(t, world, 5*time.Millisecond)
	before := BuildResult(world, 0)
	second := project(t, projector, world)
	if second.Document != nil || second.Topology.Revision != "1" {
		t.Fatal("routing and counters changed topology identity")
	}
	if len(second.Topology.Nodes) != 4 || second.Topology.Nodes[2].ID != "client/clients/0" || second.Topology.Nodes[3].ID != "client/clients/1" {
		t.Fatal("client nodes lost creation order")
	}
	if len(second.Servers) != 1 || len(second.Servers[0].Clients) != 2 ||
		second.Servers[0].Clients[0].ID != "client/clients/1" || second.Servers[0].Clients[1].ID != "client/clients/0" {
		t.Fatalf("routed clients lost connection-table order: %+v", second.Servers)
	}
	if repeated := project(t, projector, world); !reflect.DeepEqual(repeated, second) || !reflect.DeepEqual(BuildResult(world, 0), before) {
		t.Fatal("reading projection changed a counter, rate window or pending event")
	}
	if jsonProjection(t, first.Document) != frozen {
		t.Fatal("later values mutated a published definition")
	}
	for _, node := range first.Document.Nodes {
		live := second.Topology.Nodes[projector.nodeIndex[node.ID]]
		for _, metric := range node.Metrics {
			_, dynamic := live.Metrics[metric.Key]
			if dynamic == (metric.Value != nil) {
				t.Fatalf("metric %s/%s was omitted or duplicated across definitions and values", node.ID, metric.Key)
			}
		}
	}
	if first.Document.Nodes[2].Metrics[0].Key != "payloadBytes" || *first.Document.Nodes[2].Metrics[0].Value != 4096 ||
		first.Document.Nodes[2].Metrics[3].Key != "packetGapMs" || *first.Document.Nodes[2].Metrics[3].Value != 1000 {
		t.Fatal("static client values or metric ordering changed")
	}
	traffic := second.Topology.Nodes[0].Traffic
	if traffic == nil || traffic.SampleWindowSeconds != 0.005 || traffic.ReceiveBytesPerSecond <= 0 {
		t.Fatalf("traffic did not preserve the completed sampling window: %+v", traffic)
	}
}

func TestProjectionRevisionsRetainHistoricalReportLinks(t *testing.T) {
	definition := projectionScenario()
	definition.Workload.ClientGroups[0].ClientGap = 2 * time.Millisecond
	world := projectionWorld(t, definition)
	projector := NewProjector("membership")
	initial := project(t, projector, world)
	frozen := jsonProjection(t, initial.Document)
	advanceProjection(t, world, 0)
	if sample := project(t, projector, world); sample.Document == nil || sample.Document.Revision != "2" {
		t.Fatal("first arrival did not revise definitions")
	}
	advanceProjection(t, world, 2*time.Millisecond)
	if sample := project(t, projector, world); sample.Document == nil || sample.Document.Revision != "3" {
		t.Fatal("second arrival did not revise definitions")
	}
	if err := world.SpawnClients(1, "clients"); err != nil {
		t.Fatal(err)
	}
	reserved := project(t, projector, world)
	if reserved.Document != nil || reserved.Topology.Nodes[0].Metrics["scheduledClients"] != 3 {
		t.Fatal("reserving a future client revised current membership")
	}
	advanceProjection(t, world, world.Now())
	spawned := project(t, projector, world)
	if spawned.Document == nil || spawned.Document.Revision != "4" {
		t.Fatal("spawned arrival did not revise definitions")
	}
	linksBefore := len(BuildResult(world, 0).Links)
	if err := world.RemoveBackend("one"); err != nil {
		t.Fatal(err)
	}
	removed := project(t, projector, world)
	if removed.Document == nil || removed.Document.Revision != "5" || len(removed.Servers) != 0 {
		t.Fatal("backend removal did not revise current membership")
	}
	if len(BuildResult(world, 0).Links) != linksBefore || len(removed.Topology.Links) != linksBefore-2 {
		t.Fatal("current graph and historical report did not apply their respective link membership")
	}
	for _, link := range removed.Document.Links {
		if link.Source == "backend/one" || link.Target == "backend/one" {
			t.Fatal("removed backend retained visible paths")
		}
	}
	if jsonProjection(t, initial.Document) != frozen {
		t.Fatal("later membership changed an older document")
	}
}

// Replacing a backend under the same ID keeps node membership unchanged. The
// additional retained physical paths reveal the change only during link visits.
func TestProjectionRebuildsDefinitionsAfterLateLinkChange(t *testing.T) {
	definition := projectionScenario()
	world := projectionWorld(t, definition)
	advanceProjection(t, world, 0)
	projector := NewProjector("late-change")
	first := project(t, projector, world)
	frozen := jsonProjection(t, first.Document)
	resolved, err := scenario.Normalize(definition)
	if err != nil {
		t.Fatal(err)
	}
	replacement := resolved.Backends[0]
	replacement.Name, replacement.Address = "Replacement", "10.99.0.1"
	if err := world.RemoveBackend(replacement.ID); err != nil {
		t.Fatal(err)
	}
	world.AddBackend(replacement)
	second := project(t, projector, world)
	if second.Document == nil || second.Document.Revision != "2" ||
		second.Document.Nodes[1].Label != "Replacement" || second.Document.Nodes[1].Address != "10.99.0.1:80" ||
		len(second.Document.Nodes) != len(first.Document.Nodes) || len(second.Document.Links) != len(first.Document.Links)+2 {
		t.Fatalf("late change did not refresh complete definitions: %+v", second.Document)
	}
	if jsonProjection(t, first.Document) != frozen {
		t.Fatal("copying a cached definition prefix mutated the original document")
	}
	if unchanged := project(t, projector, world); unchanged.Document != nil || projector.document != second.Document {
		t.Fatal("unchanged sampling replaced the cached definition document")
	}
}

func TestProjectionAggregatesCompletedFailedAndFutureTransfers(t *testing.T) {
	definition := projectionScenario()
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 64}
	longGap := time.Second
	definition.Workload.ClientGroups = []scenario.ClientGroup{
		{ID: "done", Count: 1},
		{ID: "interrupted", Count: 1},
		{ID: "ongoing", Count: 1, Stream: scenario.StreamPatch{PacketGap: &longGap}},
		{ID: "future", Count: 1, StartAt: time.Second},
	}
	world := projectionWorld(t, definition)
	advanceProjection(t, world, 0)
	if err := world.StopClient("client/interrupted/0"); err != nil {
		t.Fatal(err)
	}
	advanceProjection(t, world, 10*time.Millisecond)
	sample := project(t, NewProjector("outcomes"), world)
	if sample.Transfers.Started != 3 || sample.Transfers.NotStarted != 1 || sample.Transfers.Succeeded != 1 ||
		sample.Transfers.Failed != 1 || sample.Transfers.InProgress != 1 || sample.Transfers.Failures[contract.TransferInterrupted] != 1 {
		t.Fatalf("outcomes = %+v", sample.Transfers)
	}
	if sample.Transfers.Handshake.Samples != 2 || sample.Transfers.Payload.Samples != 1 ||
		sample.Transfers.Completion.Samples != 1 || sample.Transfers.Completion.MinSeconds <= 0 {
		t.Fatalf("duration samples = %+v", sample.Transfers)
	}
	result := BuildResult(world, 3*time.Second)
	if !reflect.DeepEqual(result.Transfers, sample.Transfers) || !reflect.DeepEqual(TransferResults(world), sample.Transfers) {
		t.Fatal("CLI and browser aggregates disagree")
	}
	if result.VirtualTime != "10ms" || result.WallTime != "3s" || result.Seed != 41 ||
		result.CompletedPayloadBytes != 64 || result.State.Algorithm != "RoundRobin" || result.State.Mode != contract.LBModeNAT {
		t.Fatalf("legacy report values changed: %+v", result)
	}
	advanceProjection(t, world, world.EndTime())
	if summary := TransferResults(world); summary.InProgress != 1 || summary.Failed != 1 || summary.NotStarted != 1 {
		t.Fatal("reaching the horizon inferred a failure for ongoing or future clients")
	}
}

func TestSnapshotIdleAndRuntimeRepresentations(t *testing.T) {
	idle, document, err := BuildSnapshot(nil, nil, RunState{Status: "idle", Algorithm: "random"})
	if err != nil || document != nil {
		t.Fatal("idle snapshot unexpectedly has definitions or an error")
	}
	want := `{"type":"simulationSnapshot","version":1,"mode":"NAT","algorithm":"Random","servers":[],"simulation":{"status":"idle","algorithm":"random","virtualTimeSeconds":0},"topology":{"revision":"0","nodes":[],"links":[]}}`
	if actual := jsonProjection(t, idle); actual != want {
		t.Fatalf("idle JSON = %s", actual)
	}
	world := projectionWorld(t, projectionScenario())
	advanceProjection(t, world, 5*time.Millisecond)
	if err := world.SetAlgorithm("random"); err != nil {
		t.Fatal(err)
	}
	active, document, err := BuildSnapshot(world, NewProjector("live"), RunState{Status: "finished", RunID: "live", Error: "sample error", Elapsed: 2 * time.Second})
	if err != nil || document == nil || active.Algorithm != "Random" || active.Simulation.Algorithm != "random" ||
		active.Simulation.Status != "finished" || active.Simulation.Error != "sample error" || active.Simulation.VirtualTimeSeconds != 0.005 {
		t.Fatalf("active snapshot = %+v, definitions = %+v, error = %v", active, document, err)
	}
	if active.Simulation.Payload == nil || active.Simulation.Transfers == nil || active.Simulation.Performance == nil ||
		active.Simulation.Performance.WallTimeSeconds != 2 || active.Simulation.Performance.EventsPerWallSecond != float64(world.EventsExecuted())/2 {
		t.Fatal("active snapshot omitted or altered a run summary")
	}
}

func TestProjectionRevisionLimitAndPerformanceZero(t *testing.T) {
	world := projectionWorld(t, projectionScenario())
	projector := NewProjector("overflow")
	projector.revision = ^uint64(0)
	if _, err := projector.Sample(world); err == nil {
		t.Fatal("revision counter wrapped instead of rejecting a new definition")
	}
	for _, elapsed := range []time.Duration{0, -time.Nanosecond} {
		if got := MeasurePerformance(elapsed, 17, 23); got.EventsExecuted != 17 || got.ForwardedPackets != 23 ||
			got.WallTimeSeconds != 0 || got.EventsPerWallSecond != 0 || got.ForwardedPacketsPerWallSecond != 0 {
			t.Fatalf("nonpositive wall time = %+v", got)
		}
	}
}
