package main

import (
	"encoding/json"
	"load-balancer/scenario"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
)

// sampleTelemetry uses the production projector's synchronous native observations.
func sampleTelemetry(t *testing.T, publisher *presentation.Projector, s *simulation.World) presentation.Sample {
	t.Helper()
	sample, err := publisher.Sample(s)
	if err != nil {
		t.Fatal(err)
	}
	return sample
}

// telemetryJSON freezes a document for immutability comparisons.
func telemetryJSON(t *testing.T, value any) []byte {
	t.Helper()
	document, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// restoreTelemetryTopology verifies every definition has exactly one live counterpart.
func restoreTelemetryTopology(t *testing.T, document *contract.TopologyDocument, live contract.TelemetryTopology) contract.Topology {
	t.Helper()
	if document.Revision != live.Revision {
		t.Fatal("definitions and values have different revisions")
	}
	nodes := make(map[string]contract.TelemetryNode, len(live.Nodes))
	for _, node := range live.Nodes {
		nodes[node.ID] = node
	}
	if len(nodes) != len(document.Nodes) {
		t.Fatal("live node membership differs from definitions")
	}
	graph := contract.Topology{
		Nodes: make([]contract.TopologyNode, 0, len(document.Nodes)),
		Links: make([]contract.TopologyLink, 0, len(document.Links)),
	}
	for _, definition := range document.Nodes {
		node, exists := nodes[definition.ID]
		if !exists {
			t.Fatalf("missing live node %q", definition.ID)
		}
		metrics := make([]contract.TopologyMetric, 0, len(definition.Metrics))
		dynamicCount := 0
		for _, metric := range definition.Metrics {
			value, dynamic := node.Metrics[metric.Key]
			if metric.Value != nil {
				if dynamic {
					t.Fatalf("static metric %q was repeated in the live frame", metric.Key)
				}
				value = *metric.Value
			} else {
				if !dynamic {
					t.Fatalf("missing dynamic metric %q on %q", metric.Key, definition.ID)
				}
				dynamicCount++
			}
			metrics = append(metrics, contract.TopologyMetric{
				Key: metric.Key, Label: metric.Label, Unit: metric.Unit, Value: value,
			})
		}
		if dynamicCount != len(node.Metrics) {
			t.Fatalf("undefined dynamic metric on %q", definition.ID)
		}
		graph.Nodes = append(graph.Nodes, contract.TopologyNode{
			ID:           definition.ID,
			Kind:         definition.Kind,
			Label:        definition.Label,
			ResourceID:   definition.ResourceID,
			GroupID:      definition.GroupID,
			Description:  definition.Description,
			Address:      definition.Address,
			Active:       node.Active,
			Metrics:      metrics,
			Traffic:      node.Traffic,
			TCPPhase:     node.TCPPhase,
			TCPState:     node.TCPState,
			CloseOutcome: node.CloseOutcome,
		})
	}
	links := make(map[string]contract.TopologyLinkStats, len(live.Links))
	for _, link := range live.Links {
		links[link.ID] = link.Stats
	}
	if len(links) != len(document.Links) {
		t.Fatal("live link membership differs from definitions")
	}
	for _, definition := range document.Links {
		stats, exists := links[definition.ID]
		if !exists {
			t.Fatalf("missing live link %q", definition.ID)
		}
		graph.Links = append(graph.Links, contract.TopologyLink{
			ID: definition.ID, Label: definition.Label,
			Source: definition.Source, Target: definition.Target, Config: definition.Config, Stats: stats,
		})
	}
	if document.Ingress != nil {
		if live.Ingress == nil {
			t.Fatal("missing shared receiver values")
		}
		queues := make(map[string]contract.TopologyLinkStats, len(live.Ingress.Queues))
		for _, queue := range live.Ingress.Queues {
			queues[queue.ID] = queue.Stats
		}
		graph.Ingress = &contract.TopologyIngress{
			Config: document.Ingress.Config,
			Stats:  live.Ingress.Stats,
			Queues: make([]contract.TopologyInputQueue, 0, len(document.Ingress.Queues)),
		}
		for _, definition := range document.Ingress.Queues {
			stats, exists := queues[definition.ID]
			if !exists {
				t.Fatalf("missing input queue %q", definition.ID)
			}
			graph.Ingress.Queues = append(graph.Ingress.Queues, contract.TopologyInputQueue{
				ID: definition.ID, Side: definition.Side, Label: definition.Label,
				Capacity: definition.Capacity, Stats: stats,
			})
		}
	}
	return graph
}

// restoreTelemetryServers checks dashboard counters and routed-client key references.
func restoreTelemetryServers(t *testing.T, document *contract.TopologyDocument, live []contract.TelemetryServer) []contract.ServerStateWithClients {
	t.Helper()
	keys := make(map[string]contract.ClientKey)
	for _, node := range document.Nodes {
		if node.ClientKey != nil {
			keys[node.ID] = *node.ClientKey
		}
	}
	definitions := make(map[string]contract.ServerDefinition, len(document.Servers))
	for _, server := range document.Servers {
		definitions[server.ID] = server
	}
	servers := make([]contract.ServerStateWithClients, 0, len(live))
	for _, server := range live {
		definition, exists := definitions[server.ID]
		if !exists {
			t.Fatalf("missing backend definition %q", server.ID)
		}
		clients := make([]contract.ClientState, 0, len(server.Clients))
		for _, client := range server.Clients {
			key, exists := keys[client.ID]
			if !exists {
				t.Fatalf("routed client %q has no defined key", client.ID)
			}
			clients = append(clients, contract.ClientState{
				Key: key, RetrySyn: client.RetrySyn, RetryData: client.RetryData,
				AcknowledgedPayloadBytes: client.AcknowledgedPayloadBytes,
				Transfer:                 client.Transfer,
				Connection:               client.Connection,
			})
		}
		servers = append(servers, contract.ServerStateWithClients{
			Server: contract.ServerState{
				ID:                     server.ID,
				Name:                   definition.Name,
				Address:                definition.Address,
				Port:                   definition.Port,
				Active:                 server.Active,
				ActiveConnections:      server.ActiveConnections,
				HalfOpenConnections:    server.HalfOpenConnections,
				LatencyP95Milli:        server.LatencyP95Milli,
				NumberOfHandledPackets: server.NumberOfHandledPackets,
				ReceivedPayloadBytes:   server.ReceivedPayloadBytes,
				CpuUsage:               server.CpuUsage,
			},
			Clients: clients,
		})
	}
	return servers
}

// TestTelemetryMatchesLegacyProjection checks complete reconstruction and pure reads.
func TestTelemetryMatchesLegacyProjection(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	{
		initialState := definition.States["0"]
		initialState.Workload.ClientGroups[0].Count = 2
		definition.States["0"] = initialState
	}
	definition.Fixed.Workload.Defaults.Bytes = 4096
	s := newScenarioSimulation(compiledRuntimeScenario(t, definition))
	runIngressUntil(t, s, 0)
	publisher := presentation.NewProjector("run-test")
	first := sampleTelemetry(t, publisher, s)
	if first.Document == nil || first.Document.RunID != "run-test" || first.Document.Revision != "1" {
		t.Fatal("the first sample must publish one run-scoped definition document")
	}
	frozenDefinition := telemetryJSON(t, first.Document)

	runIngressUntil(t, s, 500*time.Microsecond)
	s.SampleTraffic()
	before := presentation.BuildResult(s, 0)
	beforeRates := telemetryJSON(t, presentation.BuildTopology(s))
	second, err := publisher.Sample(s)
	if err != nil {
		t.Fatal(err)
	}
	if second.Document != nil || second.Topology.Revision != "1" {
		t.Fatal("counter and traffic changes must reuse existing definitions")
	}
	if actual := restoreTelemetryTopology(t, first.Document, second.Topology); !reflect.DeepEqual(actual, presentation.BuildTopology(s)) {
		t.Fatal("compact topology cannot reconstruct the previous full projection")
	}
	// Detailed host counters travel as topology metrics, not repeated server frames.
	expectedServers := append([]contract.ServerStateWithClients(nil), before.State.Servers...)
	for i := range expectedServers {
		expectedServers[i].Server.Host = nil
	}
	if actual := restoreTelemetryServers(t, first.Document, second.Servers); !reflect.DeepEqual(actual, expectedServers) {
		t.Fatal("compact server values lost counters or routed-client details")
	}
	clients := 0
	for _, server := range second.Servers {
		clients += len(server.Clients)
	}
	if clients != 2 {
		t.Fatal("the equivalence case must exercise actual routed-client references")
	}
	third := sampleTelemetry(t, publisher, s)
	if !reflect.DeepEqual(third, second) || !reflect.DeepEqual(before, presentation.BuildResult(s, 0)) {
		t.Fatal("reading telemetry changed a model, counter or pending event")
	}
	if string(beforeRates) != string(telemetryJSON(t, presentation.BuildTopology(s))) {
		t.Fatal("reading telemetry advanced the rate sampling window")
	}
	if string(frozenDefinition) != string(telemetryJSON(t, first.Document)) {
		t.Fatal("live sampling mutated the HTTP definitions")
	}

	wire := string(telemetryJSON(t, second))
	for _, repeated := range []string{`"label"`, `"config"`, `"source"`, `"address"`, `"SrcIP"`, `"payloadBytes"`} {
		if strings.Contains(wire, repeated) {
			t.Fatalf("live telemetry repeats definition metadata %s", repeated)
		}
	}
}

// TestTelemetryClosedClientsKeepRevision distinguishes lifecycle values from membership.
func TestTelemetryClosedClientsKeepRevision(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	// Keep the active closer through TIME_WAIT, then exercise retained closed values.
	msl := time.Millisecond
	definition.Fixed.TCP.Close.MSL = &msl
	s := newScenarioSimulation(compiledRuntimeScenario(t, definition))
	runIngressUntil(t, s, 0)
	publisher := presentation.NewProjector("run-closed")
	first := sampleTelemetry(t, publisher, s)
	runIngressUntil(t, s, 20*time.Millisecond)
	if s.Info().ClosedClients != 1 {
		t.Fatal("the zero-payload connection should have completed and closed")
	}
	if err := s.MarkBackendFailed(first.Document.Servers[0].ID); err != nil {
		t.Fatal(err)
	}
	second := sampleTelemetry(t, publisher, s)
	if second.Document != nil || second.Topology.Revision != first.Topology.Revision {
		t.Fatal("closed clients and health changes must not revise definitions")
	}
	if actual := restoreTelemetryTopology(t, first.Document, second.Topology); !reflect.DeepEqual(actual, presentation.BuildTopology(s)) {
		t.Fatal("closed-client or failed-backend values diverged from the full projection")
	}
}

// TestTelemetryArrivalsAndSpawnReviseDefinitions keeps every retained document immutable.
func TestTelemetryArrivalsAndTimelineGrowthReviseDefinitions(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	{
		initialState := definition.States["0"]
		initialState.Workload.ClientGroups[0].Count = 3
		definition.States["0"] = initialState
	}
	{
		initialState := definition.States["0"]
		initialState.Workload.ClientGroups[0].ClientGap = 2 * time.Millisecond
		definition.States["0"] = initialState
	}
	// Grow the lifetime birth budget at 6 ms, after the initial three arrivals.
	future := definition.States["0"]
	future.Workload.ClientGroups = append([]scenario.ClientGroup(nil), future.Workload.ClientGroups...)
	future.Workload.ClientGroups[0].Count = 5
	definition.States["6000000"] = future
	s := newScenarioSimulation(compiledRuntimeScenario(t, definition))
	publisher := presentation.NewProjector("run-arrivals")
	initial := sampleTelemetry(t, publisher, s)
	frozenInitial := telemetryJSON(t, initial.Document)
	for index, at := range []time.Duration{0, 2 * time.Millisecond, 4 * time.Millisecond} {
		runIngressUntil(t, s, at)
		sample := sampleTelemetry(t, publisher, s)
		if sample.Document == nil || sample.Topology.Revision != strconv.Itoa(index+2) {
			t.Fatalf("client arrival %d did not revise the structure", index)
		}
		clientKeys := 0
		for _, node := range sample.Document.Nodes {
			if node.ClientKey != nil {
				clientKeys++
			}
		}
		if clientKeys != index+1 {
			t.Fatal("every created client needs its own immutable key definition")
		}
	}

	runIngressUntil(t, s, 6*time.Millisecond)
	spawned := sampleTelemetry(t, publisher, s)
	if spawned.Document == nil || spawned.Topology.Revision != "5" || s.Info().ScheduledClients != 5 || s.Info().CreatedClients != 4 {
		t.Fatal("a client born after timeline growth must revise definitions")
	}
	if string(frozenInitial) != string(telemetryJSON(t, initial.Document)) {
		t.Fatal("later revisions changed a retained document")
	}
	otherRun := presentation.NewProjector("other-run")
	other := sampleTelemetry(t, otherRun, s)
	if other.Document.RunID != "other-run" || other.Document.Revision != "1" {
		t.Fatal("a fresh run must have its own definition identity and revision sequence")
	}
}

// A backend failure changes live values; adding another backend revises graph membership.
func TestTelemetryStoppedBackendsRetainLinksAndNewBackendsReviseDefinitions(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	{
		initialState := definition.States["0"]
		initialState.Workload.ClientGroups[0].Count = 0
		definition.States["0"] = initialState
	}
	resolved := compiledRuntimeScenario(t, definition)
	s := newScenarioSimulation(resolved)
	publisher := presentation.NewProjector("run-backend-lifecycle")
	first := sampleTelemetry(t, publisher, s)
	frozenFirst := telemetryJSON(t, first.Document)
	oldLinks := len(presentation.BuildResult(s, 0).Links)
	stopped := first.Document.Servers[0].ID
	if err := s.StopBackend(stopped); err != nil {
		t.Fatal(err)
	}
	second := sampleTelemetry(t, publisher, s)
	if second.Document != nil || second.Topology.Revision != "1" ||
		len(second.Servers) != len(first.Servers) || second.Servers[0].Active {
		t.Fatal("stopping a backend must update live state without revising membership")
	}
	if len(presentation.BuildResult(s, 0).Links) != oldLinks || len(second.Topology.Links) != oldLinks {
		t.Fatal("stopping a backend discarded its physical paths")
	}
	if actual := restoreTelemetryTopology(t, first.Document, second.Topology); !reflect.DeepEqual(actual, presentation.BuildTopology(s)) {
		t.Fatal("stopped backend values differ from the full projection")
	}

	// A distinct backend changes actual membership and contributes two additional paths.
	additional := initialScenarioState(t, resolved).Backends[0].NormalizedBackend
	additional.ID = "additional"
	additional.Address = "10.99.0.1"
	s.AddBackend(additional)
	third := sampleTelemetry(t, publisher, s)
	if third.Document == nil || third.Topology.Revision != "2" ||
		len(third.Servers) != len(second.Servers)+1 || len(third.Topology.Links) != oldLinks+2 {
		t.Fatal("new backend membership did not revise complete definitions")
	}
	if third.Document.Servers[len(third.Servers)-1].ID != "additional" {
		t.Fatal("additional backend definitions did not follow registration order")
	}
	if string(frozenFirst) != string(telemetryJSON(t, first.Document)) {
		t.Fatal("backend changes mutated an older HTTP document")
	}
}
