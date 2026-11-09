package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

func scenarioTimelineInput() scenario.Scenario {
	input := scenarioCLIDefinition()
	input.Fixed.Execution.Duration = 120 * time.Millisecond
	input.Fixed.Execution.SnapshotInterval = 5 * time.Millisecond
	later := input.States["0"]
	later.Resources.LoadBalancer.Algorithm = "roundRobin"
	later.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = 16_000_000
	later.Resources.Backends = append([]scenario.BackendStateConfig(nil), later.Resources.Backends...)
	later.Resources.Backends[0].Name = "after-transition"
	later.Workload.ClientGroups = append([]scenario.ClientGroup(nil), later.Workload.ClientGroups...)
	later.Workload.ClientGroups = append(later.Workload.ClientGroups, scenario.ClientGroup{
		ID: "late", Count: 1, StartAt: 40 * time.Millisecond,
		Network: scenario.EndpointNetworkConfig{Profile: "fast"},
		Stream:  scenario.StreamPatch{Bytes: scenarioRuntimeValue(19)},
	})
	input.States["40000000"] = later
	return input
}

func assertTimelineResult(t *testing.T, result contract.SimulationResult) {
	t.Helper()
	if result.State.Algorithm != "RoundRobin" || result.Ingress.Config.BandwidthBitsPerSecond != 16_000_000 {
		t.Fatalf("later LB configuration was not executed: algorithm=%s ingress=%d",
			result.State.Algorithm, result.Ingress.Config.BandwidthBitsPerSecond)
	}
	if result.CreatedClients != 4 || result.CompletedStreams != 4 ||
		result.ReceivedPayloadBytes != 4150 || result.AcknowledgedPayloadBytes != 4150 {
		t.Fatalf("later generator was not executed: clients=%d streams=%d payload=%d/%d",
			result.CreatedClients, result.CompletedStreams,
			result.ReceivedPayloadBytes, result.AcknowledgedPayloadBytes)
	}
	foundName := false
	for _, backend := range result.State.Servers {
		foundName = foundName || backend.Server.Name == "after-transition"
	}
	if !foundName {
		t.Fatal("later backend settings were lost")
	}
}

func TestScenarioCLIExecutesAndArchivesCompleteTimeline(t *testing.T) {
	executable := buildScenarioCLI(t)
	input := scenarioTimelineInput()
	path := writeScenarioCLIInput(t, input)
	report := decodeScenarioCLIReport(t, executeScenarioCLI(t, executable, "-scenario", path), "finished")
	assertTimelineResult(t, report.Result)
	assertScenarioCLIManifest(t, report.Manifest, input, false)

	// Each seed recompiles the same complete timeline into an independent world.
	directory := filepath.Join(t.TempDir(), "timeline-series")
	output := executeScenarioCLI(t, executable, "-scenario", path,
		"-seeds", "-7,11,11", "-output-dir", directory)
	if output.err != nil {
		t.Fatalf("timeline series: %v\n%s", output.err, output.stderr)
	}
	index := decodeScenarioSeriesIndex(t, output.stdout, "finished")
	assertScenarioCLIManifest(t, index.Manifest, input, false)
	var previous contract.SimulationResult
	for position, seed := range []int64{-7, 11, 11} {
		entry := index.Runs[position]
		if entry.Seed != strconv.FormatInt(seed, 10) {
			t.Fatalf("series seed = %s, want %d", entry.Seed, seed)
		}
		run := readScenarioSeriesRun(t, directory, entry.Report, "finished")
		requested := input
		requested.Fixed.Execution.Seed = seed
		assertScenarioCLIManifest(t, run.Manifest, requested, false)
		assertTimelineResult(t, run.Result)
		if run.Result.Seed != seed {
			t.Fatalf("runtime seed = %d, archived seed = %d", run.Result.Seed, seed)
		}
		run.Result.WallTime = ""
		if position == 2 && !reflect.DeepEqual(previous, run.Result) {
			t.Fatal("repeated seed inherited state from the previous timeline run")
		}
		previous = run.Result
	}

	// Invalid late settings fail before output or trace files are reserved.
	invalid := scenarioTimelineInput()
	later := invalid.States["40000000"]
	later.Resources.LoadBalancer.Algorithm = "invalid"
	invalid.States["40000000"] = later
	invalidPath := writeScenarioCLIInput(t, invalid)
	reportPath := filepath.Join(t.TempDir(), "invalid-report.json")
	tracePath := filepath.Join(t.TempDir(), "invalid.trace")
	rejected := executeScenarioCLI(t, executable, "-scenario", invalidPath,
		"-output", reportPath, "-trace", tracePath)
	assertScenarioCLIRejected(t, rejected)
	for _, path := range []string{reportPath, tracePath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid later state created %s: %v", path, err)
		}
	}
}

func TestScenarioHTTPExecutesTimelineWithMatchingProvenance(t *testing.T) {
	h := newScenarioHarness(t)
	input := scenarioTimelineInput()
	input.Fixed.Execution.Speed = 1
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	// The strict integer codec accepts decimal strings and canonicalizes them.
	request := bytes.Replace(document, []byte(`"scenarioVersion":2`), []byte(`"scenarioVersion":"2"`), 1)
	body := scenarioRequest(t, h.server, http.MethodPost, "/api/simulations", request, http.StatusAccepted)
	var receipt contract.RunReceipt
	if err := json.Unmarshal(body, &receipt); err != nil {
		t.Fatal(err)
	}
	final := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "finished"
	})
	if final.Simulation.Algorithm != "roundRobin" || final.Simulation.VirtualTimeSeconds != 0.12 ||
		final.Simulation.Payload == nil || final.Simulation.Payload.AcknowledgedBytes != 4150 ||
		final.Simulation.Transfers == nil || final.Simulation.Transfers.Succeeded != 4 {
		t.Fatalf("HTTP executed only the initial state: %+v", final.Simulation)
	}
	body = scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusOK)
	var record contract.RunRecord
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	if record.RunID != receipt.RunID || record.Status != "finished" || !bytes.Equal(record.Scenario, document) {
		t.Fatal("current run lost the admitted full timeline")
	}
	assertInteractiveExperiment(t, document, input.Fixed.Execution.Speed, record.Experiment)
	if final.Simulation.Experiment == nil || !reflect.DeepEqual(*final.Simulation.Experiment, record.Experiment) {
		t.Fatal("WS and the archived source identify different experiments")
	}
}

func TestScenarioHTTPInvalidLaterStateCannotAdmit(t *testing.T) {
	h := newScenarioHarness(t)
	input := scenarioTimelineInput()
	later := input.States["40000000"]
	later.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = -1
	input.States["40000000"] = later
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	scenarioRequest(t, h.server, http.MethodPost, "/api/simulations", document, http.StatusUnprocessableEntity)
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)
	if h.control.currentRun() != nil || h.control.contextForRun() != nil {
		t.Fatal("an invalid late state reserved a run")
	}
}

func assertNativeManualCommandsRejected(t *testing.T, control *runController) {
	t.Helper()
	for _, command := range []contract.Envelope{
		{Type: contract.SetAlgorithm, Payload: []byte(`{"name":"random"}`)},
		{Type: contract.SpawnClient, Payload: []byte(`{"count":1,"groupId":"fast"}`)},
	} {
		err := submitFrontendCommand(control, context.Background(), command, nil)
		if !errors.Is(err, simulation.ErrTimelineManualMutation) {
			t.Fatalf("scenario %s returned %v", command.Type, err)
		}
	}
	if control.contextForRun().Err() != nil {
		t.Fatal("rejected manual mutation changed the active run")
	}
}

func TestScenarioCommandsAreBlockedBeforeAndAfterWorldCreation(t *testing.T) {
	input := scenarioTimelineInput()
	plan, err := scenario.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	control := newRunController(context.Background())
	receipt, err := control.startTimeline(context.Background(), document, plan)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeManualCommandsRejected(t, control)
	if err := submitStopTestCommand(control, receipt.RunID, "stop-pending-scenario", nil); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(control.contextForRun().Err(), context.Canceled) {
		t.Fatal("Stop could not cancel a scenario run before world creation")
	}
	if err := control.applyStop(<-control.stops, func() (stopObservation, error) { return stopObservation{}, nil }); err != nil {
		t.Fatal(err)
	}

	h := newScenarioHarness(t)
	input.Fixed.Execution.Speed = 1
	input.Fixed.Execution.Duration = 2 * time.Second
	document, err = scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	scenarioRequest(t, h.server, http.MethodPost, "/api/simulations", document, http.StatusAccepted)
	running := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "running" && snapshot.Simulation.VirtualTimeSeconds > 0
	})
	assertNativeManualCommandsRejected(t, h.control)
	if err := submitStopTestCommand(h.control, running.Simulation.RunID, "stop-running-scenario", nil); err != nil {
		t.Fatal(err)
	}
	readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "idle"
	})
}

func TestScenarioInputsRejectVersionOneAndFlatWebSocketSettings(t *testing.T) {
	for _, document := range [][]byte{[]byte(`{"version":1}`), []byte(`{"durationSeconds":0,"speed":0}`)} {
		_, err := prepareStart(startRequest{command: &runtimeCommand{Payload: document}})
		if err == nil {
			t.Fatalf("obsolete input was accepted: %s", document)
		}
	}
	h := newScenarioHarness(t)
	scenarioRequest(t, h.server, http.MethodPost, "/api/simulations", []byte(`{"version":1}`), http.StatusBadRequest)
	if h.control.currentRun() != nil {
		t.Fatal("rejected obsolete input reserved a run")
	}
}
