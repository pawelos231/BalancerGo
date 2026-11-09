package main

import (
	"context"
	"encoding/json"
	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

// scenarioAPI connects HTTP to admission control without accessing live models.
// Defaults and run records are configuration documents, independent of snapshots.
type scenarioAPI struct {
	defaults    configuration
	control     *runController
	publication *httpserver.Publication
}

// preparedStart keeps runtime settings and the temporary form summary together.
// Document is set only when the legacy WS input still needs a stored scenario.
type preparedStart struct {
	scenario scenario.Normalized
	plan     *scenario.CompiledV2
	config   contract.SimulationConfig
	document []byte
}

func (api *scenarioAPI) DefaultsV2() scenario.ScenarioV2 {
	return api.defaults.scenarioDefinitionV2()
}

// StartScenarioV2 retains the complete document admitted by the HTTP compiler.
func (api *scenarioAPI) StartScenarioV2(
	ctx context.Context,
	input scenario.ScenarioV2,
	plan *scenario.CompiledV2,
) (contract.RunReceipt, error) {
	document, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		return contract.RunReceipt{}, err
	}
	receipt, err := api.control.startTimeline(ctx, document, plan)
	return contract.RunReceipt{RunID: receipt.RunID, Status: receipt.Status}, admissionError(err)
}

// CurrentRun returns initial settings and lifecycle metadata, including finished runs.
func (api *scenarioAPI) CurrentRun() *contract.RunRecord {
	record := api.control.currentRun()
	if record == nil {
		return nil
	}
	return &contract.RunRecord{
		RunID:      record.RunID,
		Status:     record.Status,
		Scenario:   record.Scenario,
		Experiment: record.Experiment,
	}
}

// CurrentTopology serves the last published graph, including finished experiments.
func (api *scenarioAPI) CurrentTopology() json.RawMessage {
	return api.publication.CurrentTopology()
}

// prepareStart keeps WS settings compatible while HTTP supplies a checked scenario.
func prepareStart(request startRequest, defaults configuration) (preparedStart, error) {
	if request.plan != nil {
		return preparedStart{plan: request.plan, config: scenarioSummaryV2(request.plan)}, nil
	}
	return prepareLegacyStart(request, defaults)
}

// The old form displays a summary; the run record retains every native state.
func scenarioSummaryV2(plan *scenario.CompiledV2) contract.SimulationConfig {
	fixed := plan.Fixed()
	initial, _ := plan.StateAt(0)
	return contract.SimulationConfig{
		Clients: plan.TotalClients(), Servers: len(initial.Backends),
		Algorithm: initial.LoadBalancer.Algorithm, Seed: fixed.Execution.Seed,
		Speed: fixed.Execution.Speed,
	}
}

func (prepared preparedStart) newWorld() (*simulation.World, error) {
	if prepared.plan != nil {
		return simulation.NewTimeline(prepared.plan)
	}
	return simulation.New(prepared.scenario), nil
}
