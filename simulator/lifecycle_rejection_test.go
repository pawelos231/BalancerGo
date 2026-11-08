package main

import (
	"bytes"
	"testing"
	"time"

	"load-balancer/internal/contract"
)

func TestLifecycleRejectedStartKeepsPreviousFinalOutcome(t *testing.T) {
	h := newScenarioHarness(t)
	input := h.defaults.scenarioDefinition()
	input.Execution.Speed, input.Execution.Duration = 0, 100*time.Millisecond
	input.Workload.Defaults.Bytes = 0
	receipt := startTelemetryScenario(t, h, input)
	var original []byte
	for {
		kind, data := readLifecycleMessage(t, h.socket)
		if kind == "simulationSnapshot" {
			continue
		}
		var ended contract.SimulationEndedFrame
		decodeLifecycleMessage(t, data, &ended)
		assertLifecycleMeasurement(t, ended, contract.SimulationFinished)
		if kind != "simulationEnded" || ended.RunID != receipt.RunID {
			t.Fatalf("unexpected first outcome: %s", data)
		}
		original = data
		break
	}

	if err := h.socket.WriteJSON(contract.Envelope{
		Type: contract.StartSimulation, CommandID: "bad-start", Payload: []byte(`{"clients":-1}`),
	}); err != nil {
		t.Fatal(err)
	}
	kind, data := readLifecycleMessage(t, h.socket)
	var rejected contract.CommandResultFrame
	decodeLifecycleMessage(t, data, &rejected)
	if kind != "commandResult" || rejected.CommandType != contract.StartSimulation || rejected.CommandID != "bad-start" ||
		rejected.Status != contract.CommandRejected || rejected.Error == "" || rejected.RunID == "" || rejected.RunID == receipt.RunID {
		t.Fatalf("invalid Start did not receive its own private rejection: %s", data)
	}
	if retained := h.control.currentRun(); retained == nil || retained.RunID != receipt.RunID || retained.Status != "finished" {
		t.Fatalf("invalid Start changed the previous run's retained record: %+v", retained)
	}

	// Rejection must preserve the complete frozen message, including its sequence and definitions.
	kind, reconnected := readLifecycleMessage(t, lifecycleSocket(t, h))
	if kind != "simulationEnded" || !bytes.Equal(reconnected, original) {
		t.Fatalf("invalid Start replaced or changed the previous final outcome: %s", reconnected)
	}
}
