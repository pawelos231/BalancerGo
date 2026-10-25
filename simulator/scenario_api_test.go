package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
	"load-balancer/scenario"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// scenarioHarness runs the real owner with HTTP and WebSocket connections.
type scenarioHarness struct {
	server      *httptest.Server
	control     *runController
	publication *httpserver.Publication
	socket      *websocket.Conn
	defaults    configuration
}

func newScenarioHarness(t *testing.T) *scenarioHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	control := newRunController(ctx)
	publication := &httpserver.Publication{}
	hub := httpserver.NewHub(func(command contract.Envelope) error {
		return submitFrontendCommand(control, ctx, command)
	})
	go hub.Run(ctx)
	defaults := legacyTestConfiguration()
	api := &scenarioAPI{defaults: defaults, control: control, publication: publication}
	server := httptest.NewServer(httpserver.NewServer(hub, "", api).Handler)
	done := make(chan error, 1)
	go func() { done <- runInteractive(ctx, defaults, hub, control, publication) }()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	socket, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		cancel()
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		socket.Close()
		cancel()
		server.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("simulation owner: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("simulation owner did not stop")
		}
	})
	return &scenarioHarness{server: server, control: control, publication: publication, socket: socket, defaults: defaults}
}

func readScenarioSnapshot(t *testing.T, socket *websocket.Conn, ready func(contract.SimulationSnapshot) bool) contract.SimulationSnapshot {
	t.Helper()
	socket.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var snapshot contract.SimulationSnapshot
		if err := socket.ReadJSON(&snapshot); err != nil {
			t.Fatal(err)
		}
		if ready(snapshot) {
			return snapshot
		}
	}
}

func scenarioRequest(t *testing.T, server *httptest.Server, method, path string, document []byte, status int) []byte {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, bytes.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s: HTTP %d, want %d: %s", method, path, response.StatusCode, status, body)
	}
	return body
}

// TestHTTPScenarioLifecycle checks profiles in the runtime and request-independent starts.
func TestHTTPScenarioLifecycle(t *testing.T) {
	h := newScenarioHarness(t)
	input := h.defaults.scenarioDefinition()
	input.Execution.Speed = 1
	input.Execution.Duration = 2 * time.Second
	slowRate := int64(10_000_000)
	input.Network.Profiles = []scenario.LinkProfile{{
		ID: "slow", Config: scenario.LinkPatch{BandwidthBitsPerSecond: &slowRate},
	}}
	input.Workload.ClientGroups = []scenario.ClientGroup{
		{ID: "slow-clients", Count: 2, Network: scenario.EndpointNetworkConfig{Profile: "slow"}},
		{ID: "fast-clients", Count: 1},
	}
	input.Resources.Backends[0].Network.Profile = "slow"
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}

	// Validation returns the original profiles and does not create a run.
	validated := scenarioRequest(t, h.server, http.MethodPost, "/api/scenarios/validate", document, http.StatusOK)
	var validation struct {
		Scenario json.RawMessage
		Summary  struct{ TotalClients int }
	}
	if err := json.Unmarshal(validated, &validation); err != nil {
		t.Fatal(err)
	}
	if validation.Summary.TotalClients != 3 {
		t.Fatal("validation did not resolve both groups")
	}
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)

	// The HTTP handler returns before the paced run finishes, closing its request context.
	body := scenarioRequest(t, h.server, http.MethodPost, "/api/simulations", document, http.StatusAccepted)
	var receipt contract.RunReceipt
	if err := json.Unmarshal(body, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.RunID == "" || receipt.Status != "accepted" {
		t.Fatalf("invalid receipt: %+v", receipt)
	}
	snapshot := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && len(snapshot.Topology.Nodes) == 6
	})
	if snapshot.Simulation.Status != "running" || h.control.contextForRun().Err() != nil {
		t.Fatal("the completed HTTP request canceled its accepted simulation")
	}

	// Both the slow backend and slow client group's real links use the profile.
	body = scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current/topology", nil, http.StatusOK)
	var structure struct {
		RunID    string `json:"runId"`
		Revision string `json:"revision"`
		Links    []struct {
			Config struct {
				BandwidthBitsPerSecond int64 `json:"bandwidthBitsPerSecond"`
			} `json:"config"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &structure); err != nil {
		t.Fatal(err)
	}
	if structure.RunID != snapshot.Simulation.RunID || structure.Revision != snapshot.Topology.Revision {
		t.Fatal("HTTP topology does not match the live run and revision")
	}
	slowLinks := 0
	fastLinks := 0
	for _, link := range structure.Links {
		if link.Config.BandwidthBitsPerSecond == slowRate {
			slowLinks++
		} else {
			fastLinks++
		}
	}
	if slowLinks != 6 || fastLinks != 4 {
		t.Fatalf("profile settings did not reach runtime links: slow=%d fast=%d", slowLinks, fastLinks)
	}
	body = scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusOK)
	var record contract.RunRecord
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	restored, err := scenario.DecodeDocument(record.Scenario)
	if err != nil || !reflect.DeepEqual(restored, input) {
		t.Fatalf("current scenario changed: %v", err)
	}
	if record.RunID != receipt.RunID || record.Status != "running" {
		t.Fatalf("wrong record: %+v", record)
	}

	// HTTP and WS cannot start another model while this one owns the slot.
	scenarioRequest(t, h.server, http.MethodPost, "/api/simulations", document, http.StatusConflict)
	err = submitFrontendCommand(h.control, context.Background(), contract.Envelope{Type: contract.StartSimulation, Payload: []byte(`{}`)})
	if !errors.Is(err, errSimulationBusy) {
		t.Fatalf("WS bypassed HTTP admission: %v", err)
	}
	if err := h.socket.WriteJSON(contract.Envelope{Type: contract.StopSimulation, Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool { return snapshot.Simulation.Status == "idle" })
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)
}

// TestScenarioStartAdmission checks simultaneous transports, detached records and Stop.
func TestScenarioStartAdmission(t *testing.T) {
	serviceCtx, cancelService := context.WithCancel(context.Background())
	defer cancelService()
	control := newRunController(serviceCtx)
	input := legacyTestConfiguration().scenarioDefinition()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}

	// Many simultaneous HTTP requests share exactly one pending start slot.
	const callers = 16
	errorsCh := make(chan error, callers)
	var workers sync.WaitGroup
	for i := 0; i < callers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := control.startScenario(context.Background(), document, resolved)
			errorsCh <- err
		}()
	}
	workers.Wait()
	close(errorsCh)
	accepted := 0
	for err := range errorsCh {
		if err == nil {
			accepted++
		} else if !errors.Is(err, errSimulationBusy) {
			t.Fatal(err)
		}
	}
	if accepted != 1 || len(control.starts) != 1 {
		t.Fatalf("accepted %d starts", accepted)
	}
	first := control.currentRun()
	first.Scenario[0] = '!'
	if control.currentRun().Scenario[0] != '{' {
		t.Fatal("HTTP record exposed mutable configuration")
	}

	// Stop interrupts the reserved run and blocks new admission until the empty snapshot.
	runCtx := control.contextForRun()
	if err := submitFrontendCommand(control, context.Background(), contract.Envelope{Type: contract.StopSimulation}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(runCtx.Err(), context.Canceled) {
		t.Fatal("Stop did not cancel the run")
	}
	control.finishRun()
	_, err = control.startScenario(context.Background(), document, resolved)
	if !errors.Is(err, errSimulationBusy) {
		t.Fatal("new Start bypassed pending Stop")
	}
	control.forgetRun()
	control.acknowledgeStop(func() error { return nil })

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	second, err := control.startScenario(requestCtx, document, resolved)
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest()
	if control.contextForRun().Err() != nil {
		t.Fatal("request cancellation stopped the accepted model")
	}
	if second.RunID == first.RunID {
		t.Fatal("new run reused its previous identity")
	}
	cancelService()
	if control.contextForRun().Err() == nil {
		t.Fatal("service shutdown did not cancel its model")
	}
}

// TestLegacyStartStillCreatesScenarioRecord checks the existing form's WS path.
func TestLegacyStartStillCreatesScenarioRecord(t *testing.T) {
	h := newScenarioHarness(t)
	if err := h.socket.WriteJSON(contract.Envelope{Type: contract.StartSimulation, Payload: []byte(`{"clients":1,"speed":0}`)}); err != nil {
		t.Fatal(err)
	}
	snapshot := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool { return snapshot.Simulation.Status == "finished" })
	if snapshot.Simulation.RunID == "" {
		t.Fatal("legacy run has no identity")
	}
	body := scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusOK)
	var record contract.RunRecord
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	input, err := scenario.DecodeDocument(record.Scenario)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "finished" || record.RunID != snapshot.Simulation.RunID || input.Workload.ClientGroups[0].Count != 1 {
		t.Fatal("legacy run was not exposed through the scenario API")
	}
}
