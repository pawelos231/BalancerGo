package main

import (
	"context"
	"encoding/json"
	"fmt"
	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"

	"github.com/gorilla/websocket"
)

// telemetryFrame retains the actual bytes so unknown wire fields cannot hide in decoding.
type telemetryFrame struct {
	snapshot contract.SimulationSnapshot
	data     []byte
}

// readTelemetryFrame waits for a complete replacement snapshot matching the predicate.
func readTelemetryFrame(
	t *testing.T,
	socket *websocket.Conn,
	ready func(contract.SimulationSnapshot) bool,
) telemetryFrame {
	t.Helper()
	if err := socket.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		_, data, err := socket.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var snapshot contract.SimulationSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatal(err)
		}
		if ready(snapshot) {
			return telemetryFrame{snapshot: snapshot, data: data}
		}
	}
}

// startTelemetryScenario uses real HTTP admission and returns its run identity.
func startTelemetryScenario(t *testing.T, h *scenarioHarness, input scenario.Scenario) contract.RunReceipt {
	t.Helper()
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	data := scenarioRequest(t, h.server, http.MethodPost, "/api/simulations", document, http.StatusAccepted)
	var receipt contract.RunReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

// topologyDocument reads the definitions without accessing a live model from HTTP.
func topologyDocument(t *testing.T, h *scenarioHarness) contract.TopologyDocument {
	t.Helper()
	data := scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current/topology", nil, http.StatusOK)
	var document contract.TopologyDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

// stopTelemetryScenario waits for the empty state before checking the HTTP cache.
func stopTelemetryScenario(t *testing.T, h *scenarioHarness) {
	t.Helper()
	command := contract.Envelope{Type: contract.StopSimulation, Payload: []byte(`{}`)}
	if err := h.socket.WriteJSON(command); err != nil {
		t.Fatal(err)
	}
	frame := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "idle"
	})
	if len(frame.snapshot.Topology.Nodes) != 0 || len(frame.snapshot.Topology.Links) != 0 {
		t.Fatal("Stop retained topology measurements")
	}
	if frame.snapshot.Simulation.RunID != "" || frame.snapshot.Topology.Ingress != nil {
		t.Fatal("Stop retained the old run or shared ingress")
	}
	if frame.snapshot.Simulation.Payload != nil {
		t.Fatal("Stop retained the previous run's payload totals")
	}
	if frame.snapshot.Simulation.Transfers != nil {
		t.Fatal("Stop retained the previous run's transfer results")
	}
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current/topology", nil, http.StatusNotFound)
}

// assertTelemetryReferences checks the tuple and every resource ID in the snapshot.
func assertTelemetryReferences(t *testing.T, snapshot contract.SimulationSnapshot, document contract.TopologyDocument) {
	t.Helper()
	if document.RunID != snapshot.Simulation.RunID || document.Revision != snapshot.Topology.Revision {
		t.Fatalf("definitions %s/%s do not match snapshot %s/%s",
			document.RunID, document.Revision, snapshot.Simulation.RunID, snapshot.Topology.Revision)
	}
	if len(document.Nodes) != len(snapshot.Topology.Nodes) || len(document.Links) != len(snapshot.Topology.Links) {
		t.Fatal("definition and measurement memberships disagree")
	}
	nodes := make(map[string]bool, len(document.Nodes))
	for _, node := range document.Nodes {
		if nodes[node.ID] {
			t.Fatalf("duplicate definition node %q", node.ID)
		}
		nodes[node.ID] = true
		if node.Kind == contract.TopologyNodeClient && node.ClientKey == nil {
			t.Fatalf("client definition %q has no restorable flow key", node.ID)
		}
	}
	links := make(map[string]bool, len(document.Links))
	for _, link := range document.Links {
		if !nodes[link.Source] || !nodes[link.Target] || links[link.ID] {
			t.Fatalf("invalid directed link definition %q", link.ID)
		}
		links[link.ID] = true
	}
	for _, node := range snapshot.Topology.Nodes {
		if !nodes[node.ID] {
			t.Fatalf("measurement references undefined node %q", node.ID)
		}
	}
	for _, link := range snapshot.Topology.Links {
		if !links[link.ID] {
			t.Fatalf("measurement references undefined link %q", link.ID)
		}
	}
	for _, server := range snapshot.Servers {
		for _, client := range server.Clients {
			if !nodes[client.ID] {
				t.Fatalf("routed client %q has no definition", client.ID)
			}
		}
	}
}

// assertCompactTelemetry inspects raw JSON, including fields unknown to the Go DTO.
func assertCompactTelemetry(t *testing.T, frame telemetryFrame) {
	t.Helper()
	if frame.snapshot.Type != "simulationSnapshot" || frame.snapshot.Version != 1 {
		t.Fatal("snapshot lacks the supported wire version")
	}
	forbidden := map[string]bool{
		"config": true, "kind": true, "label": true, "description": true,
		"address": true, "source": true, "target": true, "groupId": true,
		"resourceId": true, "clientKey": true, "Key": true, "SrcIP": true,
	}
	var value any
	if err := json.Unmarshal(frame.data, &value); err != nil {
		t.Fatal(err)
	}
	var inspect func(any, string)
	inspect = func(value any, path string) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if forbidden[key] {
					t.Errorf("static field repeated in WS: %s.%s", path, key)
				}
				inspect(child, path+"."+key)
			}
		case []any:
			for index, child := range value {
				inspect(child, fmt.Sprintf("%s[%d]", path, index))
			}
		}
	}
	inspect(value, "snapshot")
}

// TestTelemetryTopologyRevisionLifecycle separates structural changes from counters.
func TestTelemetryTopologyRevisionLifecycle(t *testing.T) {
	h := newScenarioHarness(t)
	input := h.defaults.scenarioDefinition()
	input.Execution.Duration = 2 * time.Second
	input.Execution.Speed = 1
	input.Execution.SnapshotInterval = 20 * time.Millisecond
	input.Workload.Defaults.Bytes = 1 << 30
	input.Workload.ClientGroups = []scenario.ClientGroup{
		{ID: "initial", Count: 1},
		{ID: "later", Count: 1, StartAt: 500 * time.Millisecond},
	}
	receipt := startTelemetryScenario(t, h, input)
	first := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && len(snapshot.Topology.Nodes) == 4
	})
	initialRevision := first.snapshot.Topology.Revision
	assertTelemetryReferences(t, first.snapshot, topologyDocument(t, h))
	assertCompactTelemetry(t, first)

	// Metrics and runtime routing policy do not create new resource definitions.
	next := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.VirtualTimeSeconds > first.snapshot.Simulation.VirtualTimeSeconds
	})
	if next.snapshot.Topology.Revision != initialRevision || reflect.DeepEqual(next.data, first.data) {
		t.Fatal("changing measurements did not retain a stable definition revision")
	}
	command := contract.Envelope{Type: contract.SetAlgorithm, Payload: []byte(`{"Name":"random"}`)}
	if err := h.socket.WriteJSON(command); err != nil {
		t.Fatal(err)
	}
	changed := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Algorithm == "random"
	})
	if changed.snapshot.Topology.Revision != initialRevision {
		t.Fatal("runtime algorithm changed the static graph revision")
	}

	// Spawn publishes its new definition before the corresponding live snapshot.
	command = contract.Envelope{Type: contract.SpawnClient, Payload: []byte(`{"Count":1,"groupId":"initial"}`)}
	if err := h.socket.WriteJSON(command); err != nil {
		t.Fatal(err)
	}
	spawned := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return len(snapshot.Topology.Nodes) == 5
	})
	if spawned.snapshot.Simulation.RunID != receipt.RunID || spawned.snapshot.Topology.Revision == initialRevision {
		t.Fatal("Spawn lost the run identity or retained an outdated definition")
	}
	assertTelemetryReferences(t, spawned.snapshot, topologyDocument(t, h))
	copy := h.publication.CurrentTopology()
	copy[0] = '!'
	if h.publication.CurrentTopology()[0] != '{' {
		t.Fatal("HTTP topology cache exposed mutable bytes")
	}

	// A scheduled arrival changes membership independently of runtime commands.
	later := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return len(snapshot.Topology.Nodes) == 6
	})
	if later.snapshot.Topology.Revision == spawned.snapshot.Topology.Revision {
		t.Fatal("scheduled client arrival did not update the graph revision")
	}
	assertTelemetryReferences(t, later.snapshot, topologyDocument(t, h))
	stopTelemetryScenario(t, h)

	// A fresh model may reuse revision numbers, but never its previous run identity.
	second := startTelemetryScenario(t, h, input)
	fresh := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == second.RunID && len(snapshot.Topology.Nodes) == 4
	})
	if second.RunID == receipt.RunID || fresh.snapshot.Topology.Revision != initialRevision {
		t.Fatal("run and revision tuple did not identify a fresh model")
	}
	assertTelemetryReferences(t, fresh.snapshot, topologyDocument(t, h))
	stopTelemetryScenario(t, h)
}

// TestTelemetryFinishedRunCanReconnect keeps the last sample and its definitions together.
func TestTelemetryFinishedRunCanReconnect(t *testing.T) {
	h := newScenarioHarness(t)
	input := h.defaults.scenarioDefinition()
	input.Execution.Duration = 150 * time.Millisecond
	input.Execution.Speed = 0
	input.Workload.Defaults.Bytes = 0
	input.Workload.ClientGroups[0].Count = 1
	receipt := startTelemetryScenario(t, h, input)
	finished := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "finished"
	})
	assertCompactTelemetry(t, finished)
	document := topologyDocument(t, h)
	assertTelemetryReferences(t, finished.snapshot, document)

	// A new browser receives the frozen replacement frame without earlier messages.
	url := "ws" + strings.TrimPrefix(h.server.URL, "http") + "/ws"
	socket, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	reconnected := readTelemetryFrame(t, socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "finished"
	})
	if !reflect.DeepEqual(reconnected.snapshot, finished.snapshot) {
		t.Fatal("reconnect changed the frozen final frame")
	}
	if !reflect.DeepEqual(topologyDocument(t, h), document) {
		t.Fatal("reconnect changed the frozen resource definitions")
	}
	stopTelemetryScenario(t, h)
}

// TestTelemetryPayloadSize compares actual JSON for the same active model state.
func TestTelemetryPayloadSize(t *testing.T) {
	cfg := legacyTestConfiguration()
	cfg.clients = 2000
	cfg.streamBytes = 1 << 30
	cfg.queue = 4096
	cfg.ingress.QueueCapacity = 4096
	cfg.maxEvents = 1_000_000
	s := newSimulation(cfg)
	if err := s.Advance(context.Background(), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	s.SampleTraffic()
	state := presentation.BuildResult(s, 0).State
	graph := presentation.BuildTopology(s)
	const runID = "payload-measurement"
	publisher := presentation.NewProjector(runID)
	sample, err := publisher.Sample(s)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Document == nil {
		t.Fatal("first sample has no resource definitions")
	}

	// Reproduce the previous full WS envelope without changing model counters.
	legacy := struct {
		contract.LoadBalancerState
		Simulation struct {
			Status             string                    `json:"status"`
			RunID              string                    `json:"runId"`
			Config             contract.SimulationConfig `json:"config"`
			VirtualTimeSeconds float64                   `json:"virtualTimeSeconds"`
		} `json:"simulation"`
		Topology contract.Topology `json:"topology"`
	}{LoadBalancerState: state, Topology: graph}
	legacy.Simulation.Status = "running"
	legacy.Simulation.RunID = runID
	legacy.Simulation.Config = cfg.publicConfig()
	legacy.Simulation.VirtualTimeSeconds = s.Now().Seconds()
	current := contract.SimulationSnapshot{
		Type:      "simulationSnapshot",
		Version:   1,
		Mode:      state.Mode,
		Algorithm: state.Algorithm,
		Servers:   sample.Servers,
		Simulation: contract.SimulationMetadata{
			Status:             "running",
			RunID:              runID,
			Algorithm:          cfg.algorithm,
			VirtualTimeSeconds: s.Now().Seconds(),
		},
		Topology: sample.Topology,
	}
	before := encodedTelemetrySize(t, legacy)
	after := encodedTelemetrySize(t, current)
	definitions := encodedTelemetrySize(t, sample.Document)
	if after >= before {
		t.Fatalf("compact snapshot has %d bytes; previous snapshot had %d", after, before)
	}

	// Charge the full HTTP document once, then compare one hundred metric frames.
	const frames = 100
	oldTotal := before * frames
	newTotal := definitions + after*frames
	routed := 0
	for _, backend := range state.Servers {
		routed += len(backend.Clients)
	}
	t.Logf("active fixture: %d created clients, %d routed, %d nodes, %d links at %s",
		s.Info().CreatedClients, routed, len(graph.Nodes), len(graph.Links), s.Now())
	t.Logf("JSON bytes: previous WS=%d, compact WS=%d, HTTP definitions=%d; WS reduction=%.2f%%",
		before, after, definitions, 100*(1-float64(after)/float64(before)))
	t.Logf("100 frames + one definition: previous=%d bytes, compact=%d bytes; reduction=%.2f%%",
		oldTotal, newTotal, 100*(1-float64(newTotal)/float64(oldTotal)))
}

// encodedTelemetrySize measures JSON bytes without HTTP or WebSocket framing overhead.
func encodedTelemetrySize(t *testing.T, value any) int {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return len(data)
}
