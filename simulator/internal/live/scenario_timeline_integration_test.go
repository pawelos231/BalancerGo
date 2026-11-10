package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/internal/testsupport"
	"load-balancer/scenario"
)

func TestScenarioHTTPExecutesTimelineWithMatchingProvenance(t *testing.T) {
	h := newScenarioHarness(t)
	input := testsupport.TimelineScenario()
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
	input := testsupport.TimelineScenario()
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
	input := testsupport.TimelineScenario()
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
