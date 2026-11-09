package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

type nativeScenarioServiceStub struct {
	scenarioServiceStub
	definitionV2 scenario.ScenarioV2
	inputV2      scenario.ScenarioV2
	plan         *scenario.CompiledV2
	nativeStarts int
}

func (service *nativeScenarioServiceStub) DefaultsV2() scenario.ScenarioV2 {
	return service.definitionV2
}

func (service *nativeScenarioServiceStub) StartScenarioV2(
	ctx context.Context,
	input scenario.ScenarioV2,
	plan *scenario.CompiledV2,
) (contract.RunReceipt, error) {
	service.nativeStarts++
	service.startContext = ctx
	service.inputV2, service.plan = input, plan
	return contract.RunReceipt{RunID: "run-native"}, service.startError
}

func scenarioHTTPDefinitionV2() scenario.ScenarioV2 {
	base := scenarioHTTPDefinition()
	initial := base.States["0"]
	later := initial
	later.Resources.LoadBalancer.Algorithm = "random"
	later.Workload.ClientGroups = append([]scenario.ClientGroup(nil), initial.Workload.ClientGroups...)
	later.Workload.ClientGroups[0].Count = 6
	base.States["500000000"] = later
	return base
}

func encodedHTTPScenarioV2(t *testing.T, definition scenario.ScenarioV2) []byte {
	t.Helper()
	document, err := scenario.EncodeDocumentV2(definition)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestScenarioV2StartPassesCompleteCompiledTimeline(t *testing.T) {
	definition := scenarioHTTPDefinitionV2()
	document := encodedHTTPScenarioV2(t, definition)
	service := &nativeScenarioServiceStub{}
	response := requestScenarioAPI(scenarioHTTPHandler(service), http.MethodPost,
		"/api/simulations", "application/json", document)
	if response.Code != http.StatusAccepted || service.nativeStarts != 1 || service.startCalls != 0 {
		t.Fatalf("native admission = %d, native calls %d, legacy calls %d: %s",
			response.Code, service.nativeStarts, service.startCalls, response.Body.String())
	}
	if !bytes.Equal(encodedHTTPScenarioV2(t, service.inputV2), document) {
		t.Fatal("admission changed the complete source document")
	}
	state, err := service.plan.StateAt(750 * time.Millisecond)
	if err != nil || state.LoadBalancer.Algorithm != "random" || state.ClientGroups[0].Count != 6 {
		t.Fatalf("later state was lost before admission: %+v, %v", state, err)
	}
	if len(service.plan.Transitions()) != 1 || service.plan.TotalClients() != 8 {
		t.Fatal("HTTP supplied only the initial state")
	}
}

func TestScenarioV2InvalidLaterStateCannotStart(t *testing.T) {
	definition := scenarioHTTPDefinitionV2()
	later := definition.States["500000000"]
	later.Resources.LoadBalancer.Algorithm = "invalid"
	definition.States["500000000"] = later
	invalid := encodedHTTPScenarioV2(t, definition)
	valid := encodedHTTPScenarioV2(t, scenarioHTTPDefinitionV2())
	unknownField := bytes.Replace(valid, []byte(`"500000000":{`), []byte(`"500000000":{"unknown":1,`), 1)
	for _, test := range []struct {
		name   string
		body   []byte
		status int
	}{
		{name: "invalid settings", body: invalid, status: http.StatusUnprocessableEntity},
		{name: "unknown field", body: unknownField, status: http.StatusBadRequest},
	} {
		for _, path := range []string{"/api/scenarios/validate", "/api/simulations"} {
			t.Run(test.name+path, func(t *testing.T) {
				service := &nativeScenarioServiceStub{}
				response := requestScenarioAPI(scenarioHTTPHandler(service), http.MethodPost,
					path, "application/json", test.body)
				if response.Code != test.status || service.nativeStarts != 0 || service.startCalls != 0 {
					t.Fatalf("invalid later state admission = %d: %s", response.Code, response.Body.String())
				}
			})
		}
	}
}

func TestScenarioV2ValidationAndDefaultsPreserveAllStates(t *testing.T) {
	definition := scenarioHTTPDefinitionV2()
	definition.Fixed.Network.Defaults.BandwidthBitsPerSecond = 9_007_199_254_740_993
	service := &nativeScenarioServiceStub{
		scenarioServiceStub: scenarioServiceStub{definition: scenarioHTTPDefinition()},
		definitionV2:        definition,
	}
	handler := scenarioHTTPHandler(service)
	document := encodedHTTPScenarioV2(t, definition)
	response := requestScenarioAPI(handler, http.MethodPost, "/api/scenarios/validate", "application/json", document)
	var validation contract.ScenarioValidationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &validation); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !bytes.Equal(validation.Scenario, document) {
		t.Fatalf("native validation lost its source: %d %s", response.Code, response.Body.String())
	}
	if validation.Summary.TotalClients != 8 || validation.Summary.BackendCount != 2 {
		t.Fatalf("summary = %+v", validation.Summary)
	}
	resolved, err := scenario.NormalizeV2(definition)
	if err != nil {
		t.Fatal(err)
	}
	wantResolved, err := scenario.EncodeNormalizedDocumentV2(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(validation.Resolved, wantResolved) {
		t.Fatal("resolved response did not retain the checked timeline")
	}
	var view struct {
		States []struct {
			Backends []struct {
				ID                string `json:"id"`
				PhysicalAvailable bool   `json:"physicalAvailable"`
				Network           struct {
					ToLoadBalancer struct {
						Bandwidth json.RawMessage `json:"bandwidthBitsPerSecond"`
					} `json:"toLoadBalancer"`
				} `json:"network"`
			} `json:"backends"`
		} `json:"states"`
	}
	if err := json.Unmarshal(validation.Resolved, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.States) != 2 || len(view.States[0].Backends) != 2 {
		t.Fatal("resolved response lost backend declarations")
	}
	backend := view.States[0].Backends[0]
	if backend.ID != "a" || !backend.PhysicalAvailable || string(backend.Network.ToLoadBalancer.Bandwidth) != `"9007199254740993"` {
		t.Fatal("resolved backend fields must be flat and preserve exact integers")
	}
	defaults := requestScenarioAPI(handler, http.MethodGet, "/api/scenarios/defaults?version=2", "", nil)
	if defaults.Code != http.StatusOK || !bytes.Equal(bytes.TrimSpace(defaults.Body.Bytes()), document) {
		t.Fatalf("native defaults = %d: %s", defaults.Code, defaults.Body.String())
	}
	unqualified := requestScenarioAPI(handler, http.MethodGet, "/api/scenarios/defaults", "", nil)
	if _, err := scenario.DecodeDocumentV2(unqualified.Body.Bytes()); unqualified.Code != http.StatusOK || err != nil {
		t.Fatalf("unqualified defaults must return native v2: %d %s", unqualified.Code, unqualified.Body.String())
	}
}
