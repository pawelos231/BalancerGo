package main

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	httpserver "load-balancer/http"
	"load-balancer/model"
)

// sampleTelemetry reads the same detached backend state used by production publishing.
func sampleTelemetry(t *testing.T, publisher *telemetryPublisher, s *simulation) telemetrySample {
	t.Helper()
	sample, err := publisher.sample(s, s.lb.Tick())
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
func restoreTelemetryTopology(t *testing.T, document *model.TopologyDocument, live model.TelemetryTopology) model.Topology {
	t.Helper()
	if document.Revision != live.Revision {
		t.Fatal("definitions and values have different revisions")
	}
	nodes := make(map[string]model.TelemetryNode, len(live.Nodes))
	for _, node := range live.Nodes {
		nodes[node.ID] = node
	}
	if len(nodes) != len(document.Nodes) {
		t.Fatal("live node membership differs from definitions")
	}
	graph := model.Topology{
		Nodes: make([]model.TopologyNode, 0, len(document.Nodes)),
		Links: make([]model.TopologyLink, 0, len(document.Links)),
	}
	for _, definition := range document.Nodes {
		node, exists := nodes[definition.ID]
		if !exists {
			t.Fatalf("missing live node %q", definition.ID)
		}
		metrics := make([]model.TopologyMetric, 0, len(definition.Metrics))
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
			metrics = append(metrics, model.TopologyMetric{
				Key: metric.Key, Label: metric.Label, Unit: metric.Unit, Value: value,
			})
		}
		if dynamicCount != len(node.Metrics) {
			t.Fatalf("undefined dynamic metric on %q", definition.ID)
		}
		graph.Nodes = append(graph.Nodes, model.TopologyNode{
			ID:          definition.ID,
			Kind:        definition.Kind,
			Label:       definition.Label,
			ResourceID:  definition.ResourceID,
			GroupID:     definition.GroupID,
			Description: definition.Description,
			Address:     definition.Address,
			Active:      node.Active,
			Metrics:     metrics,
			Traffic:     node.Traffic,
		})
	}
	links := make(map[string]model.TopologyLinkStats, len(live.Links))
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
		graph.Links = append(graph.Links, model.TopologyLink{
			ID: definition.ID, Label: definition.Label,
			Source: definition.Source, Target: definition.Target, Config: definition.Config, Stats: stats,
		})
	}
	if document.Ingress != nil {
		if live.Ingress == nil {
			t.Fatal("missing shared receiver values")
		}
		queues := make(map[string]model.TopologyLinkStats, len(live.Ingress.Queues))
		for _, queue := range live.Ingress.Queues {
			queues[queue.ID] = queue.Stats
		}
		graph.Ingress = &model.TopologyIngress{
			Config: document.Ingress.Config,
			Stats:  live.Ingress.Stats,
			Queues: make([]model.TopologyInputQueue, 0, len(document.Ingress.Queues)),
		}
		for _, definition := range document.Ingress.Queues {
			stats, exists := queues[definition.ID]
			if !exists {
				t.Fatalf("missing input queue %q", definition.ID)
			}
			graph.Ingress.Queues = append(graph.Ingress.Queues, model.TopologyInputQueue{
				ID: definition.ID, Side: definition.Side, Label: definition.Label,
				Capacity: definition.Capacity, Stats: stats,
			})
		}
	}
	return graph
}

// restoreTelemetryServers checks dashboard counters and routed-client key references.
func restoreTelemetryServers(t *testing.T, document *model.TopologyDocument, live []model.TelemetryServer) []model.ServerStateWithClients {
	t.Helper()
	keys := make(map[string]model.ClientKey)
	for _, node := range document.Nodes {
		if node.ClientKey != nil {
			keys[node.ID] = *node.ClientKey
		}
	}
	definitions := make(map[string]model.ServerDefinition, len(document.Servers))
	for _, server := range document.Servers {
		definitions[server.ID] = server
	}
	servers := make([]model.ServerStateWithClients, 0, len(live))
	for _, server := range live {
		definition, exists := definitions[server.ID]
		if !exists {
			t.Fatalf("missing backend definition %q", server.ID)
		}
		clients := make([]model.ClientState, 0, len(server.Clients))
		for _, client := range server.Clients {
			key, exists := keys[client.ID]
			if !exists {
				t.Fatalf("routed client %q has no defined key", client.ID)
			}
			clients = append(clients, model.ClientState{
				Key: key, RetrySyn: client.RetrySyn, RetryData: client.RetryData,
				AcknowledgedPayloadBytes: client.AcknowledgedPayloadBytes,
				Transfer:                 client.Transfer,
			})
		}
		servers = append(servers, model.ServerStateWithClients{
			Server: model.ServerState{
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
	definition.Workload.ClientGroups[0].Count = 2
	definition.Workload.Defaults.Bytes = 4096
	s := newScenarioSimulation(normalizedRuntimeScenario(t, definition))
	runIngressUntil(t, s, 0)
	publisher := newTelemetryPublisher("run-test")
	first := sampleTelemetry(t, publisher, s)
	if first.Document == nil || first.Document.RunID != "run-test" || first.Document.Revision != "1" {
		t.Fatal("the first sample must publish one run-scoped definition document")
	}
	frozenDefinition := telemetryJSON(t, first.Document)

	runIngressUntil(t, s, 500*time.Microsecond)
	s.sampleTraffic()
	state := s.lb.Tick()
	before := s.result(0)
	beforeRates := telemetryJSON(t, s.traffic.rates)
	second, err := publisher.sample(s, state)
	if err != nil {
		t.Fatal(err)
	}
	if second.Document != nil || second.Topology.Revision != "1" {
		t.Fatal("counter and traffic changes must reuse existing definitions")
	}
	if actual := restoreTelemetryTopology(t, first.Document, second.Topology); !reflect.DeepEqual(actual, s.topology(state)) {
		t.Fatal("compact topology cannot reconstruct the previous full projection")
	}
	if actual := restoreTelemetryServers(t, first.Document, second.Servers); !reflect.DeepEqual(actual, state.Servers) {
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
	if !reflect.DeepEqual(third, second) || !reflect.DeepEqual(before, s.result(0)) {
		t.Fatal("reading telemetry changed a model, counter or pending event")
	}
	if string(beforeRates) != string(telemetryJSON(t, s.traffic.rates)) {
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
	s := newScenarioSimulation(normalizedRuntimeScenario(t, definition))
	runIngressUntil(t, s, 0)
	publisher := newTelemetryPublisher("run-closed")
	first := sampleTelemetry(t, publisher, s)
	runIngressUntil(t, s, 20*time.Millisecond)
	if !s.clients[0].Client.IsClosed() {
		t.Fatal("the zero-payload connection should have completed and closed")
	}
	s.lb.GetServers()[0].MarkFailed()
	if err := s.applyCommand(httpserver.Envelope{
		Type: httpserver.SetAlgorithm, Payload: []byte(`{"Name":"random"}`),
	}); err != nil {
		t.Fatal(err)
	}
	second := sampleTelemetry(t, publisher, s)
	if second.Document != nil || second.Topology.Revision != first.Topology.Revision {
		t.Fatal("closed clients, health changes and routing policy must not revise definitions")
	}
	if actual := restoreTelemetryTopology(t, first.Document, second.Topology); !reflect.DeepEqual(actual, s.topology(s.lb.Tick())) {
		t.Fatal("closed-client or failed-backend values diverged from the full projection")
	}
}

// TestTelemetryArrivalsAndSpawnReviseDefinitions keeps every retained document immutable.
func TestTelemetryArrivalsAndSpawnReviseDefinitions(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	definition.Workload.ClientGroups[0].Count = 3
	definition.Workload.ClientGroups[0].ClientGap = 2 * time.Millisecond
	s := newScenarioSimulation(normalizedRuntimeScenario(t, definition))
	publisher := newTelemetryPublisher("run-arrivals")
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

	if err := s.applyCommand(httpserver.Envelope{
		Type: httpserver.SpawnClient, Payload: []byte(`{"Count":2,"groupId":"clients"}`),
	}); err != nil {
		t.Fatal(err)
	}
	reserved := sampleTelemetry(t, publisher, s)
	if reserved.Document != nil || reserved.Topology.Nodes[0].Metrics["scheduledClients"] != 5 {
		t.Fatal("reserving future clients changes a live count, not current membership")
	}
	runIngressUntil(t, s, s.engine.Now())
	spawned := sampleTelemetry(t, publisher, s)
	if spawned.Document == nil || spawned.Topology.Revision != "5" {
		t.Fatal("a newly created spawned client must revise definitions")
	}
	if string(frozenInitial) != string(telemetryJSON(t, initial.Document)) {
		t.Fatal("later revisions changed a retained document")
	}
	otherRun := newTelemetryPublisher("other-run")
	other := sampleTelemetry(t, otherRun, s)
	if other.Document.RunID != "other-run" || other.Document.Revision != "1" {
		t.Fatal("a fresh run must have its own definition identity and revision sequence")
	}
}

// TestTelemetryRemovedBackendsFiltersHistoricalLinks checks IDs as well as counts.
func TestTelemetryRemovedBackendsFiltersHistoricalLinks(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	definition.Workload.ClientGroups[0].Count = 0
	s := newScenarioSimulation(normalizedRuntimeScenario(t, definition))
	publisher := newTelemetryPublisher("run-removal")
	first := sampleTelemetry(t, publisher, s)
	frozenFirst := telemetryJSON(t, first.Document)
	oldLinks := len(s.links)
	removed := s.lb.GetServers()[0]
	s.lb.RemoveServer(removed)
	second := sampleTelemetry(t, publisher, s)
	if second.Document == nil || second.Topology.Revision != "2" || len(s.links) != oldLinks {
		t.Fatal("backend removal must revise the graph without discarding historical link objects")
	}
	for _, link := range second.Document.Links {
		if link.Source == backendNodeID(removed.ID) || link.Target == backendNodeID(removed.ID) {
			t.Fatal("removed backend paths leaked into current definitions")
		}
	}
	if actual := restoreTelemetryTopology(t, second.Document, second.Topology); !reflect.DeepEqual(actual, s.topology(s.lb.Tick())) {
		t.Fatal("filtered live paths differ from the full projection")
	}

	// Replace the remaining backend before sampling: the count stays the same.
	s.lb.RemoveServer(s.lb.GetServers()[0])
	replacement := s.scenario.Backends[0]
	replacement.ID = "replacement"
	replacement.Address = "10.99.0.1"
	s.addBackend(replacement)
	third := sampleTelemetry(t, publisher, s)
	if third.Document == nil || third.Topology.Revision != "3" || len(third.Servers) != len(second.Servers) {
		t.Fatal("same-count backend replacement must still revise definitions")
	}
	if third.Document.Servers[0].ID != "replacement" {
		t.Fatal("replacement server definitions did not follow actual membership")
	}
	if string(frozenFirst) != string(telemetryJSON(t, first.Document)) {
		t.Fatal("backend changes mutated an older HTTP document")
	}
}

// TestTelemetryRejectsUndefinedClients prevents a frame the browser cannot hydrate.
func TestTelemetryRejectsUndefinedClients(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	s := newScenarioSimulation(normalizedRuntimeScenario(t, definition))
	publisher := newTelemetryPublisher("run-invalid")
	state := s.lb.Tick()
	state.Servers[0].Clients = append(state.Servers[0].Clients, model.ClientState{
		Key: model.ClientKey{SrcIP: "unregistered"},
	})
	if _, err := publisher.sample(s, state); err == nil {
		t.Fatal("undefined routed clients must fail rather than send unusable IDs")
	}
	publisher = newTelemetryPublisher("run-overflow")
	publisher.revision = ^uint64(0)
	if _, err := publisher.sample(s, s.lb.Tick()); err == nil {
		t.Fatal("the revision sequence must not wrap to zero")
	}
}
