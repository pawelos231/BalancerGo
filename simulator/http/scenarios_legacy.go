package httpserver

import (
	"context"
	"encoding/json"
	"net/http"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

// LegacyScenarioService keeps the existing browser working until E1.14.
// Only this HTTP boundary accepts the old document shape.
type LegacyScenarioService interface {
	Defaults() scenario.Scenario
	StartScenario(context.Context, scenario.Scenario, scenario.Normalized) (contract.RunReceipt, error)
}

type legacyScenario struct {
	definition scenario.Scenario
	resolved   scenario.Normalized
}

func isLegacyScenarioVersion(version json.RawMessage) bool {
	switch string(version) {
	case "", "0", "1":
		return true
	}
	var text string
	if err := json.Unmarshal(version, &text); err != nil {
		return false
	}
	return text == "0" || text == "1"
}

func readLegacyScenario(w http.ResponseWriter, document []byte) (checkedScenario, bool) {
	definition, err := scenario.DecodeDocument(document)
	if err != nil {
		writeScenarioError(w, http.StatusBadRequest, err.Error())
		return checkedScenario{}, false
	}
	resolved, err := scenario.Normalize(definition)
	if err != nil {
		writeScenarioError(w, http.StatusUnprocessableEntity, err.Error())
		return checkedScenario{}, false
	}
	return checkedScenario{legacy: &legacyScenario{definition: definition, resolved: resolved}}, true
}

func validateLegacyScenario(w http.ResponseWriter, checked legacyScenario) {
	document, err := scenario.EncodeDocument(checked.definition)
	if err != nil {
		writeScenarioError(w, http.StatusInternalServerError, "unable to encode scenario")
		return
	}
	resolved, err := scenario.EncodeNormalizedDocument(checked.resolved)
	if err != nil {
		writeScenarioError(w, http.StatusInternalServerError, "unable to encode resolved scenario")
		return
	}
	writeScenarioJSON(w, http.StatusOK, contract.ScenarioValidationResponse{
		Scenario: document,
		Resolved: resolved,
		Summary: contract.ScenarioSummary{
			TotalClients:     checked.resolved.TotalClients,
			BackendCount:     len(checked.resolved.Backends),
			ClientGroupCount: len(checked.resolved.ClientGroups),
		},
	})
}

func writeLegacyDefaults(w http.ResponseWriter, service LegacyScenarioService) {
	document, err := scenario.EncodeDocument(service.Defaults())
	if err != nil {
		writeScenarioError(w, http.StatusInternalServerError, "unable to encode default scenario")
		return
	}
	writeScenarioJSON(w, http.StatusOK, json.RawMessage(document))
}

func startLegacyScenario(ctx context.Context, service ScenarioService, checked legacyScenario) (contract.RunReceipt, error) {
	legacy, ok := service.(LegacyScenarioService)
	if !ok {
		return contract.RunReceipt{}, ErrServiceClosed
	}
	return legacy.StartScenario(ctx, checked.definition, checked.resolved)
}
