package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

const maxScenarioBytes = 2 * 1024 * 1024

// Admission errors distinguish owner conflicts from temporary unavailability.
var (
	ErrSimulationBusy   = errors.New("simulation is already running or a request is pending")
	ErrCommandQueueFull = errors.New("simulation command queue is full")
	ErrServiceClosed    = errors.New("simulation service is closed")
)

// ScenarioService admits work to the simulation owner without touching its model.
// The request context applies to admission, not the accepted run's lifetime.
type ScenarioService interface {
	Defaults() scenario.Scenario
	StartScenario(context.Context, scenario.Scenario, scenario.Normalized) (contract.RunReceipt, error)
	CurrentRun() *contract.RunRecord
}

// scenarioAPI translates bounded HTTP documents into calls on the owner service.
type scenarioAPI struct {
	service ScenarioService
}

// mountScenarioRoutes keeps the optional API on the server's private mux.
func mountScenarioRoutes(mux *http.ServeMux, service ScenarioService) {
	api := scenarioAPI{service: service}
	mux.HandleFunc("/api/scenarios/validate", scenarioRoute(http.MethodPost, api.validate))
	mux.HandleFunc("/api/scenarios/defaults", scenarioRoute(http.MethodGet, api.defaults))
	mux.HandleFunc("/api/simulations", scenarioRoute(http.MethodPost, api.start))
	mux.HandleFunc("/api/simulations/current", scenarioRoute(http.MethodGet, api.current))
}

// scenarioRoute allows the separate local visualizer to call the JSON API.
func scenarioRoute(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", method+", OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != method {
			w.Header().Set("Allow", method+", OPTIONS")
			writeScenarioError(w, http.StatusMethodNotAllowed, "method is not allowed")
			return
		}
		next(w, r)
	}
}

// validate returns a canonical document while leaving the current run untouched.
func (api scenarioAPI) validate(w http.ResponseWriter, r *http.Request) {
	definition, resolved, ok := readScenario(w, r)
	if !ok {
		return
	}
	document, err := scenario.EncodeDocument(definition)
	if err != nil {
		writeScenarioError(w, http.StatusInternalServerError, "unable to encode scenario")
		return
	}
	resolvedDocument, err := scenario.EncodeNormalizedDocument(resolved)
	if err != nil {
		writeScenarioError(w, http.StatusInternalServerError, "unable to encode resolved scenario")
		return
	}

	response := contract.ScenarioValidationResponse{
		Scenario: document,
		Resolved: resolvedDocument,
		Summary: contract.ScenarioSummary{
			TotalClients:     resolved.TotalClients,
			BackendCount:     len(resolved.Backends),
			ClientGroupCount: len(resolved.ClientGroups),
		},
	}
	writeScenarioJSON(w, http.StatusOK, response)
}

// defaults serves the owner's scenario in the same format accepted by POST.
func (api scenarioAPI) defaults(w http.ResponseWriter, r *http.Request) {
	document, err := scenario.EncodeDocument(api.service.Defaults())
	if err != nil {
		writeScenarioError(w, http.StatusInternalServerError, "unable to encode default scenario")
		return
	}
	writeScenarioJSON(w, http.StatusOK, json.RawMessage(document))
}

// start validates first, then asks the owner to admit a new run.
func (api scenarioAPI) start(w http.ResponseWriter, r *http.Request) {
	definition, resolved, ok := readScenario(w, r)
	if !ok {
		return
	}
	receipt, err := api.service.StartScenario(r.Context(), definition, resolved)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrSimulationBusy):
			status = http.StatusConflict
		case errors.Is(err, ErrCommandQueueFull), errors.Is(err, ErrServiceClosed):
			status = http.StatusServiceUnavailable
		}
		writeScenarioError(w, status, err.Error())
		return
	}

	receipt.Status = "accepted"
	writeScenarioJSON(w, http.StatusAccepted, receipt)
}

// current returns the immutable request, even after its run has finished.
func (api scenarioAPI) current(w http.ResponseWriter, r *http.Request) {
	record := api.service.CurrentRun()
	if record == nil {
		writeScenarioError(w, http.StatusNotFound, "no current simulation")
		return
	}
	writeScenarioJSON(w, http.StatusOK, record)
}

// readScenario separates malformed documents from valid JSON with invalid settings.
func readScenario(w http.ResponseWriter, r *http.Request) (scenario.Scenario, scenario.Normalized, bool) {
	var empty scenario.Scenario
	var unresolved scenario.Normalized
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeScenarioError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return empty, unresolved, false
	}

	body := http.MaxBytesReader(w, r.Body, maxScenarioBytes)
	defer body.Close()
	document, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeScenarioError(w, http.StatusRequestEntityTooLarge, "scenario exceeds the 2 MiB request limit")
		} else {
			writeScenarioError(w, http.StatusBadRequest, "unable to read scenario")
		}
		return empty, unresolved, false
	}
	definition, err := scenario.DecodeDocument(document)
	if err != nil {
		writeScenarioError(w, http.StatusBadRequest, err.Error())
		return empty, unresolved, false
	}
	resolved, err := scenario.Normalize(definition)
	if err != nil {
		writeScenarioError(w, http.StatusUnprocessableEntity, err.Error())
		return empty, unresolved, false
	}
	return definition, resolved, true
}

// writeScenarioError keeps HTTP failures separate from WebSocket command errors.
func writeScenarioError(w http.ResponseWriter, status int, message string) {
	writeScenarioJSON(w, status, contract.ErrorResponse{Error: message})
}

// writeScenarioJSON encodes before writing headers so failures stay valid JSON.
func writeScenarioJSON(w http.ResponseWriter, status int, value any) {
	document, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		document = []byte(`{"error":"unable to encode response"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(document, '\n'))
}
