package httpserver

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"load-balancer/internal/contract"
)

// Publication detaches both the owner's document and each reader's byte slice.
// An encoding failure must leave the last complete revision available.
func TestPublicationDetachedDocumentAndFailedReplacement(t *testing.T) {
	var publication Publication
	if publication.CurrentTopology() != nil {
		t.Fatal("a new publication must have no topology")
	}
	document := &contract.TopologyDocument{
		RunID:    "run-3",
		Revision: "9007199254740993",
		Nodes:    []contract.TopologyNodeDefinition{},
		Links:    []contract.TopologyLinkDefinition{},
		Servers:  []contract.ServerDefinition{},
	}
	if err := publication.PublishTopology(document); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"runId":"run-3","revision":"9007199254740993","nodes":[],"links":[],"servers":[]}`)
	document.Revision = "changed"
	read := publication.CurrentTopology()
	if !bytes.Equal(read, want) {
		t.Fatalf("published topology = %s, want %s", read, want)
	}
	read[0] = '!'
	if !bytes.Equal(publication.CurrentTopology(), want) {
		t.Fatal("a reader changed the stored document")
	}
	invalidValue := math.NaN()
	document.Nodes = []contract.TopologyNodeDefinition{{
		Metrics: []contract.TopologyMetricDefinition{{Key: "invalid", Value: &invalidValue}},
	}}
	if err := publication.PublishTopology(document); err == nil {
		t.Fatal("a nonfinite topology metric must fail encoding")
	}
	if !bytes.Equal(publication.CurrentTopology(), want) {
		t.Fatal("failed encoding replaced the previous complete revision")
	}
	publication.ClearTopology()
	if publication.CurrentTopology() != nil {
		t.Fatal("clear must remove the topology")
	}
}

type topologyServiceStub struct {
	scenarioServiceStub
	document json.RawMessage
	reads    int
}

func (service *topologyServiceStub) CurrentTopology() json.RawMessage {
	service.reads++
	return service.document
}

// A topology read serves cached bytes, including opaque revision strings.
func TestTopologyRouteLatestDocumentAndClear(t *testing.T) {
	service := &topologyServiceStub{}
	handler := NewServer(NewHub(nil), "", service).Handler
	path := "/api/simulations/current/topology"

	read := func(status int) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != status {
			t.Fatalf("GET topology: %d, want %d: %s", response.Code, status, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatal("topology responses must remain fresh and accessible from the visualizer")
		}
		return response
	}

	// Idle has no graph; finished runs remain readable until the owner clears it.
	read(http.StatusNotFound)
	service.document = json.RawMessage(`{"runId":"run-2","revision":"9007199254740993","nodes":[],"links":[],"servers":[]}`)
	response := read(http.StatusOK)
	if response.Body.String() != string(service.document)+"\n" {
		t.Fatal("the route changed the cached document")
	}
	service.document = json.RawMessage(`{"runId":"run-2","revision":"9007199254740994","nodes":[],"links":[],"servers":[]}`)
	read(http.StatusOK)
	service.document = nil
	read(http.StatusNotFound)
}

// Preflight and unsupported methods never ask the model owner for data.
func TestTopologyRouteMethods(t *testing.T) {
	service := &topologyServiceStub{}
	handler := NewServer(NewHub(nil), "", service).Handler
	for _, test := range []struct {
		method string
		status int
	}{
		{http.MethodOptions, http.StatusNoContent},
		{http.MethodPost, http.StatusMethodNotAllowed},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, "/api/simulations/current/topology", nil))
		if response.Code != test.status || service.reads != 0 {
			t.Fatalf("%s: status=%d reads=%d", test.method, response.Code, service.reads)
		}
		if response.Header().Get("Access-Control-Allow-Methods") != "GET, OPTIONS" {
			t.Fatal("missing topology method policy")
		}
	}
}
