package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
