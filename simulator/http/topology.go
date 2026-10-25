package httpserver

import (
	"encoding/json"
	"net/http"
	"sync/atomic"

	"load-balancer/internal/contract"
)

type topologyFrame struct {
	data json.RawMessage
}

// Publication stores an immutable encoded topology independently of admission.
// The simulation owner publishes a definition before broadcasting its first frame.
// Its zero value is ready to use.
type Publication struct {
	topology atomic.Pointer[topologyFrame]
}

func (p *Publication) PublishTopology(document *contract.TopologyDocument) error {
	data, err := json.Marshal(document)
	if err != nil {
		return err
	}
	p.topology.Store(&topologyFrame{data: data})
	return nil
}

// CurrentTopology returns independent bytes so callers cannot change the cache.
func (p *Publication) CurrentTopology() json.RawMessage {
	if frame := p.topology.Load(); frame != nil {
		return append(json.RawMessage(nil), frame.data...)
	}
	return nil
}

func (p *Publication) ClearTopology() {
	p.topology.Store(nil)
}

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
