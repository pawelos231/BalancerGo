package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"load-balancer/internal/contract"

	"github.com/gorilla/websocket"
)

func assertPublicationSequence(t *testing.T, frame telemetryFrame, want string) {
	t.Helper()
	if got := frame.snapshot.Simulation.PublicationSequence; got != want {
		t.Fatalf("publication sequence = %q, want %q", got, want)
	}
	var wire struct {
		Simulation map[string]json.RawMessage `json:"simulation"`
	}
	if err := json.Unmarshal(frame.data, &wire); err != nil {
		t.Fatal(err)
	}
	encoded, present := wire.Simulation["publicationSequence"]
	if want == "" {
		if present {
			t.Fatalf("idle snapshot retained publicationSequence: %s", encoded)
		}
		return
	}
	if string(encoded) != `"`+want+`"` {
		t.Fatalf("wire publication sequence = %s, want JSON string %q", encoded, want)
	}
}

func TestPublicationSequenceLifecycle(t *testing.T) {
	h := newScenarioHarness(t)
	idle := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "idle"
	})
	assertPublicationSequence(t, idle, "")

	input := h.defaults.scenarioDefinition()
	input.Fixed.Execution.Duration = time.Nanosecond
	input.Fixed.Execution.Speed = 0
	input.Fixed.Execution.SnapshotInterval = time.Hour
	{
		initialState := input.States["0"]
		initialState.Workload.ClientGroups[0].Count = 0
		input.States["0"] = initialState
	}
	receipt := startTelemetryScenario(t, h, input)
	finished := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "finished"
	})
	// A tiny unpaced run publishes start, end-of-world, and finished. The
	// final number counts even frames replaced before a browser can read them.
	assertPublicationSequence(t, finished, "3")
	if finished.snapshot.Simulation.VirtualTimeSeconds != time.Nanosecond.Seconds() {
		t.Fatal("tiny run ended at the wrong virtual time")
	}

	url := "ws" + strings.TrimPrefix(h.server.URL, "http") + "/ws"
	socket, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	replayed := readTelemetryFrame(t, socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "finished"
	})
	assertPublicationSequence(t, replayed, "3")
	if !bytes.Equal(replayed.data, finished.data) {
		t.Fatal("reconnect changed the cached publication")
	}

	// A new run starts its own sequence even when it replaces a finished model.
	second := startTelemetryScenario(t, h, input)
	if second.RunID == receipt.RunID {
		t.Fatal("new run reused the previous run identity")
	}
	fresh := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == second.RunID && snapshot.Simulation.Status == "finished"
	})
	assertPublicationSequence(t, fresh, "3")

	if err := h.socket.WriteJSON(contract.Envelope{
		Type: contract.StopSimulation, RunID: second.RunID, CommandID: "stop-sequence",
	}); err != nil {
		t.Fatal(err)
	}
	idle = readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "idle"
	})
	assertPublicationSequence(t, idle, "")
	if idle.snapshot.Simulation.RunID != "" {
		t.Fatal("Stop retained the previous run identity")
	}
}

func TestPublicationSequenceIncludesFailedSnapshot(t *testing.T) {
	h := newScenarioHarness(t)
	input := h.defaults.scenarioDefinition()
	input.Fixed.Execution.Duration = time.Second
	input.Fixed.Execution.Speed = 0
	input.Fixed.Execution.SnapshotInterval = time.Hour
	input.Fixed.Execution.MaxEvents = 1
	{
		initialState := input.States["0"]
		initialState.Workload.ClientGroups[0].Count = 1
		input.States["0"] = initialState
	}
	receipt := startTelemetryScenario(t, h, input)
	failed := readTelemetryFrame(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "failed"
	})
	// Exhausting the event budget publishes failure after the initial frame.
	assertPublicationSequence(t, failed, "2")
	if failed.snapshot.Simulation.Error == "" {
		t.Fatal("failed publication omitted its error")
	}
}
