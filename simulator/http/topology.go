package httpserver

import (
	"encoding/json"
	"net/http"
)

// TopologyService exposes a detached graph definition, never live simulation state.
// The document identifies both its run and the revision of its nodes and links.
type TopologyService interface {
	CurrentTopology() json.RawMessage
}

// mountTopologyRoute serves only the latest published definition; old revisions
// are replaced rather than retained for browsers that have fallen behind.
func mountTopologyRoute(mux *http.ServeMux, service TopologyService) {
	serve := func(w http.ResponseWriter, r *http.Request) {
		document := service.CurrentTopology()
		if len(document) == 0 {
			writeScenarioError(w, http.StatusNotFound, "no published simulation topology")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(document)
		_, _ = w.Write([]byte("\n"))
	}
	mux.HandleFunc("/api/simulations/current/topology", scenarioRoute(http.MethodGet, serve))
}
