package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

// scenarioServiceStub records admission without creating a simulation model.
type scenarioServiceStub struct {
	definition    scenario.Scenario
	record        *contract.RunRecord
	startError    error
	startCalls    int
	defaultsCalls int
	currentCalls  int
	startContext  context.Context
	input         scenario.Scenario
	plan          *scenario.Compiled
	resolved      scenario.Normalized
}

func (service *scenarioServiceStub) Defaults() scenario.Scenario {
	service.defaultsCalls++
	return service.definition
}

func (service *scenarioServiceStub) StartScenario(
	ctx context.Context,
	input scenario.Scenario,
	plan *scenario.Compiled,
) (contract.RunReceipt, error) {
	service.startCalls++
	service.startContext = ctx
	service.input, service.plan = input, plan
	service.resolved, _ = scenario.Normalize(input)
	return contract.RunReceipt{RunID: "run-17"}, service.startError
}

func (service *scenarioServiceStub) CurrentRun() *contract.RunRecord {
	service.currentCalls++
	return service.record
}

// scenarioHTTPDefinition includes multiple compact client groups and backends.
func scenarioHTTPDefinition() scenario.Scenario {
	link := scenario.LinkConfig{BandwidthBitsPerSecond: 1_000_000_000, PacketTravelTime: time.Millisecond, QueueCapacity: 4}
	return scenario.Scenario{ScenarioVersion: 2,
		Fixed: scenario.FixedConfig{
			Execution:    scenario.ExecutionConfig{Duration: time.Second, SnapshotInterval: 100 * time.Millisecond, Seed: 17, MaxEvents: 100_000},
			LoadBalancer: scenario.LoadBalancerIdentity{ID: "lb"}, Network: scenario.NetworkConfig{Defaults: link},
			Workload: scenario.WorkloadDefaultsConfig{Defaults: scenario.StreamConfig{Packets: 1, Bytes: -1}},
		},
		States: map[string]scenario.StateConfig{"0": {
			Resources: scenario.StateResourcesConfig{
				LoadBalancer: scenario.LoadBalancerStateConfig{Algorithm: "roundRobin", Ingress: link, ForwardingBitsPerSecond: 1_000_000_000},
				Backends:     []scenario.BackendStateConfig{{ID: "a", Address: "10.0.0.1", Port: 80}, {ID: "b", Address: "10.0.0.2", Port: 80}},
			},
			Workload: scenario.StateWorkloadConfig{ClientGroups: []scenario.ClientGroup{{ID: "first", Count: 3}, {ID: "second", Count: 2}}},
		}},
	}
}

func encodedHTTPScenario(t *testing.T, definition scenario.Scenario) []byte {
	t.Helper()
	document, err := scenario.EncodeDocument(definition)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// scenarioHTTPHandler mounts the API on the same private mux used in production.
func scenarioHTTPHandler(service ScenarioService) http.Handler {
	hub := NewHub(func(contract.Envelope, func(contract.CommandResultFrame)) error { return nil })
	return NewServer(hub, "", service).Handler
}

// requestScenarioAPI leaves real network sockets and the simulation owner unused.
func requestScenarioAPI(
	handler http.Handler,
	method, path, contentType string,
	body []byte,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// TestScenarioValidatePreservesExactDocument checks canonical JSON and compact totals.
func TestScenarioValidatePreservesExactDocument(t *testing.T) {
	definition := scenarioHTTPDefinition()
	definition.Fixed.Execution.Seed = math.MinInt64 + 1
	definition.Fixed.Execution.MaxEvents = math.MaxUint64
	definition.Fixed.Network.Defaults.PacketTravelTime += 7 * time.Nanosecond
	document := encodedHTTPScenario(t, definition)
	service := &scenarioServiceStub{definition: scenarioHTTPDefinition()}
	response := requestScenarioAPI(
		scenarioHTTPHandler(service),
		http.MethodPost,
		"/api/scenarios/validate",
		"application/json; charset=utf-8",
		document,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("validate status = %d: %s", response.Code, response.Body.String())
	}

	var result struct {
		Scenario json.RawMessage          `json:"scenario"`
		Summary  contract.ScenarioSummary `json:"summary"`
		Resolved json.RawMessage          `json:"resolved"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	wantSummary := contract.ScenarioSummary{
		TotalClients:     5,
		BackendCount:     2,
		ClientGroupCount: 2,
	}
	if result.Summary != wantSummary {
		t.Fatalf("summary = %+v, want %+v", result.Summary, wantSummary)
	}
	decoded, err := scenario.DecodeDocument(result.Scenario)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Fixed.Execution.Seed != definition.Fixed.Execution.Seed {
		t.Fatal("HTTP validation rounded a signed integer")
	}
	if decoded.Fixed.Execution.MaxEvents != definition.Fixed.Execution.MaxEvents {
		t.Fatal("HTTP validation rounded an unsigned integer")
	}
	if decoded.Fixed.Network.Defaults.PacketTravelTime != definition.Fixed.Network.Defaults.PacketTravelTime {
		t.Fatal("HTTP validation rounded a duration")
	}
	if !bytes.Equal(encodedHTTPScenario(t, decoded), document) {
		t.Fatal("validated document changed scenario settings")
	}

	// The effective view resolves inheritance without replacing the exportable input.
	resolved, err := scenario.Normalize(definition)
	if err != nil {
		t.Fatal(err)
	}
	expectedResolved, err := scenario.EncodeNormalizedDocument(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.Resolved, expectedResolved) {
		t.Fatalf("resolved view differs from the checked runtime settings: %s", result.Resolved)
	}
	var view struct {
		Fixed struct {
			Execution struct {
				Seed      string `json:"seed"`
				MaxEvents string `json:"maxEvents"`
			} `json:"execution"`
		} `json:"fixed"`
		States []struct {
			ClientGroups []struct {
				Stream struct {
					Bytes int `json:"bytes"`
				} `json:"stream"`
			} `json:"clientGroups"`
		} `json:"states"`
	}
	if err := json.Unmarshal(result.Resolved, &view); err != nil {
		t.Fatal(err)
	}
	if view.Fixed.Execution.Seed != "-9223372036854775807" || view.Fixed.Execution.MaxEvents != "18446744073709551615" {
		t.Fatal("the effective view rounded native integer limits")
	}
	if len(view.States[0].ClientGroups) != 2 || view.States[0].ClientGroups[0].Stream.Bytes != 1024 {
		t.Fatal("the effective view must resolve stream bytes and retain compact groups")
	}
	if service.startCalls != 0 || service.currentCalls != 0 || service.defaultsCalls != 0 {
		t.Fatal("validation accessed the running simulation service")
	}
}

// TestScenarioStartPassesValidatedSettings checks admission and request context only.
func TestScenarioStartPassesValidatedSettings(t *testing.T) {
	definition := scenarioHTTPDefinition()
	service := &scenarioServiceStub{}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/simulations",
		bytes.NewReader(encodedHTTPScenario(t, definition)),
	)
	request.Header.Set("Content-Type", "application/json")
	type contextMarker struct{}
	request = request.WithContext(context.WithValue(request.Context(), contextMarker{}, "admission"))
	response := httptest.NewRecorder()
	scenarioHTTPHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("start status = %d: %s", response.Code, response.Body.String())
	}
	var receipt contract.RunReceipt
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.RunID != "run-17" || receipt.Status != "accepted" {
		t.Fatalf("unexpected admission receipt: %+v", receipt)
	}
	if service.startCalls != 1 || service.resolved.TotalClients != 5 {
		t.Fatal("start did not pass the normalized scenario exactly once")
	}
	if service.resolved.States[0].ClientGroups[0].Stream.Bytes != 1024 {
		t.Fatal("HTTP admission passed unresolved stream settings")
	}
	if service.startContext.Value(contextMarker{}) != "admission" {
		t.Fatal("request context was not forwarded to admission")
	}
	if !bytes.Equal(encodedHTTPScenario(t, service.input), encodedHTTPScenario(t, definition)) {
		t.Fatal("start changed the original scenario document")
	}
}

// TestScenarioRejectsInvalidRequests checks both write endpoints before admission.
func TestScenarioRejectsInvalidRequests(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		status      int
	}{
		{
			name:   "missing content type",
			body:   "{}",
			status: http.StatusUnsupportedMediaType,
		},
		{
			name:        "wrong content type",
			contentType: "text/plain",
			body:        "{}",
			status:      http.StatusUnsupportedMediaType,
		},
		{
			name:        "malformed JSON",
			contentType: "application/json",
			body:        "{",
			status:      http.StatusBadRequest,
		},
		{
			name:        "trailing JSON",
			contentType: "application/json",
			body:        "{} {}",
			status:      http.StatusBadRequest,
		},
		{
			name:        "unknown field",
			contentType: "application/json",
			body:        `{"unexpected":1}`,
			status:      http.StatusBadRequest,
		},
		{
			name:        "unscoped version",
			contentType: "application/json",
			body:        `{"version":2}`,
			status:      http.StatusBadRequest,
		},
		{
			name:        "mixed version fields",
			contentType: "application/json",
			body:        `{"scenarioVersion":2,"version":2}`,
			status:      http.StatusBadRequest,
		},
		{
			name:        "duplicate field",
			contentType: "application/json",
			body:        `{"scenarioVersion":1,"scenarioVersion":1}`,
			status:      http.StatusBadRequest,
		},
		{
			name:        "unsafe numeric integer",
			contentType: "application/json",
			body:        `{"execution":{"seed":9007199254740993}}`,
			status:      http.StatusBadRequest,
		},
		{
			name:        "invalid settings",
			contentType: "application/json",
			body:        `{"scenarioVersion":2}`,
			status:      http.StatusUnprocessableEntity,
		},
		{
			name:        "body too large",
			contentType: "application/json",
			body:        strings.Repeat(" ", maxScenarioBytes+1),
			status:      http.StatusRequestEntityTooLarge,
		},
	}
	for _, path := range []string{"/api/scenarios/validate", "/api/simulations"} {
		for _, test := range cases {
			t.Run(path+"/"+test.name, func(t *testing.T) {
				service := &scenarioServiceStub{}
				response := requestScenarioAPI(
					scenarioHTTPHandler(service),
					http.MethodPost,
					path,
					test.contentType,
					[]byte(test.body),
				)
				if response.Code != test.status {
					t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
				}
				var result struct{ Error string }
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Error == "" {
					t.Fatalf("failure did not contain a JSON error: %s", response.Body.String())
				}
				if service.startCalls != 0 {
					t.Fatal("invalid scenario reached owner admission")
				}
			})
		}
	}
}

// TestScenarioStartAcceptsLargerDocuments bypasses the compact WebSocket limit.
func TestScenarioStartAcceptsLargerDocuments(t *testing.T) {
	definition := scenarioHTTPDefinition()
	group := &definition.States["0"].Workload.ClientGroups[0]
	group.Count = 1000
	for index := 0; index < group.Count; index++ {
		payloadBytes := index
		group.Overrides = append(group.Overrides, scenario.ClientOverride{
			Index: index,
			Stream: scenario.StreamPatch{
				Bytes: &payloadBytes,
			},
		})
	}
	document := encodedHTTPScenario(t, definition)
	if len(document) <= maxCommandBytes || len(document) > maxScenarioBytes {
		t.Fatalf("fixture size = %d; expected between the WS and HTTP limits", len(document))
	}
	service := &scenarioServiceStub{}
	response := requestScenarioAPI(
		scenarioHTTPHandler(service),
		http.MethodPost,
		"/api/simulations",
		"application/json",
		document,
	)
	if response.Code != http.StatusAccepted {
		t.Fatalf("large scenario rejected: %d %s", response.Code, response.Body.String())
	}
	if service.startCalls != 1 || len(service.resolved.States[0].ClientGroups[0].Overrides) != 1000 {
		t.Fatal("large request did not reach admission with all individual overrides")
	}
}

// TestScenarioBodyLimitAllowsExactBoundary distinguishes bounded input from truncation.
func TestScenarioBodyLimitAllowsExactBoundary(t *testing.T) {
	document := encodedHTTPScenario(t, scenarioHTTPDefinition())
	document = append(document, bytes.Repeat([]byte(" "), maxScenarioBytes-len(document))...)
	response := requestScenarioAPI(
		scenarioHTTPHandler(&scenarioServiceStub{}),
		http.MethodPost,
		"/api/scenarios/validate",
		"application/json",
		document,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("exact 2 MiB document rejected: %d %s", response.Code, response.Body.String())
	}
}

// TestScenarioAdmissionErrorsKeepTheirHTTPStatus checks wrapped owner rejections.
func TestScenarioAdmissionErrorsKeepTheirHTTPStatus(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{
			name:   "busy",
			err:    ErrSimulationBusy,
			status: http.StatusConflict,
		},
		{
			name:   "queue full",
			err:    ErrCommandQueueFull,
			status: http.StatusServiceUnavailable,
		},
		{
			name:   "closed",
			err:    ErrServiceClosed,
			status: http.StatusServiceUnavailable,
		},
		{
			name:   "unexpected failure",
			err:    errors.New("owner failed"),
			status: http.StatusInternalServerError,
		},
	}
	document := encodedHTTPScenario(t, scenarioHTTPDefinition())
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service := &scenarioServiceStub{startError: fmt.Errorf("admission: %w", test.err)}
			response := requestScenarioAPI(
				scenarioHTTPHandler(service),
				http.MethodPost,
				"/api/simulations",
				"application/json",
				document,
			)
			if response.Code != test.status || service.startCalls != 1 {
				t.Fatalf("admission status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

// TestScenarioReadRoutesReturnExactDocuments checks defaults and persisted run states.
func TestScenarioReadRoutesReturnExactDocuments(t *testing.T) {
	definition := scenarioHTTPDefinition()
	document := encodedHTTPScenario(t, definition)
	service := &scenarioServiceStub{definition: definition}
	handler := scenarioHTTPHandler(service)
	defaults := requestScenarioAPI(handler, http.MethodGet, "/api/scenarios/defaults", "", nil)
	if defaults.Code != http.StatusOK {
		t.Fatalf("defaults status = %d: %s", defaults.Code, defaults.Body.String())
	}
	decoded, err := scenario.DecodeDocument(defaults.Body.Bytes())
	if err != nil || !bytes.Equal(encodedHTTPScenario(t, decoded), document) {
		t.Fatalf("defaults did not return the direct scenario: %s", defaults.Body.String())
	}
	empty := requestScenarioAPI(handler, http.MethodGet, "/api/simulations/current", "", nil)
	if empty.Code != http.StatusNotFound {
		t.Fatalf("empty current status = %d", empty.Code)
	}

	// Lifecycle states retain the exact accepted request after completion or failure.
	for _, status := range []string{"accepted", "running", "finished", "failed"} {
		service.record = &contract.RunRecord{RunID: "run-17", Status: status, Scenario: document}
		response := requestScenarioAPI(handler, http.MethodGet, "/api/simulations/current", "", nil)
		var result contract.RunRecord
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || result.RunID != "run-17" || result.Status != status {
			t.Fatalf("current run changed its lifecycle: %s", response.Body.String())
		}
		if !bytes.Equal(result.Scenario, document) {
			t.Fatal("current run changed its canonical scenario")
		}
	}
	if service.startCalls != 0 || service.defaultsCalls != 1 || service.currentCalls != 5 {
		t.Fatal("read routes called the wrong owner operation")
	}
}

// TestScenarioReadFailuresReturnCompleteJSON checks response encoding before headers.
func TestScenarioReadFailuresReturnCompleteJSON(t *testing.T) {
	definition := scenarioHTTPDefinition()
	definition.Fixed.Execution.Speed = math.NaN()
	service := &scenarioServiceStub{
		definition: definition,
		record: &contract.RunRecord{
			RunID:    "run-17",
			Status:   "finished",
			Scenario: json.RawMessage("{"),
		},
	}
	handler := scenarioHTTPHandler(service)
	for _, path := range []string{"/api/scenarios/defaults", "/api/simulations/current"} {
		response := requestScenarioAPI(handler, http.MethodGet, path, "", nil)
		if response.Code != http.StatusInternalServerError || !json.Valid(response.Body.Bytes()) {
			t.Fatalf("encoding failure leaked a partial response on %s: %d %s", path, response.Code, response.Body.String())
		}
	}
}

// TestScenarioMethodsAndCORS checks browser preflight and optional API mounting.
func TestScenarioMethodsAndCORS(t *testing.T) {
	handler := scenarioHTTPHandler(&scenarioServiceStub{})
	routes := map[string]string{
		"/api/scenarios/validate":  http.MethodPost,
		"/api/scenarios/defaults":  http.MethodGet,
		"/api/simulations":         http.MethodPost,
		"/api/simulations/current": http.MethodGet,
	}
	for path, method := range routes {
		preflight := requestScenarioAPI(handler, http.MethodOptions, path, "", nil)
		if preflight.Code != http.StatusNoContent || preflight.Body.Len() != 0 {
			t.Fatalf("preflight failed on %s: %d", path, preflight.Code)
		}
		if preflight.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatal("cross-port local visualizer is not allowed")
		}
		if preflight.Header().Get("Access-Control-Allow-Methods") != method+", OPTIONS" {
			t.Fatalf("wrong allowed method on %s", path)
		}
		if preflight.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("API permits caching on %s", path)
		}
		wrong := requestScenarioAPI(handler, http.MethodDelete, path, "", nil)
		if wrong.Code != http.StatusMethodNotAllowed || wrong.Header().Get("Allow") != method+", OPTIONS" {
			t.Fatalf("wrong method did not return 405 and Allow on %s", path)
		}
	}

	// Existing callers retain their WebSocket/health server without the new service.
	legacy := NewServer(NewHub(func(contract.Envelope, func(contract.CommandResultFrame)) error {
		return nil
	}), "").Handler
	health := requestScenarioAPI(legacy, http.MethodGet, "/healthz", "", nil)
	missing := requestScenarioAPI(legacy, http.MethodGet, "/api/scenarios/defaults", "", nil)
	if health.Code != http.StatusNoContent || missing.Code != http.StatusNotFound {
		t.Fatal("optional API changed the legacy server routes")
	}
}
