package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"load-balancer/internal/contract"
)

// Read actual envelopes: a terminal outcome must never look like ordinary telemetry.
func readLifecycleMessage(t *testing.T, socket *websocket.Conn) (string, []byte) {
	t.Helper()
	if err := socket.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, data, err := socket.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Type string }
	decodeLifecycleMessage(t, data, &envelope)
	return envelope.Type, data
}

func decodeLifecycleMessage(t *testing.T, data []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("invalid lifecycle message %s: %v", data, err)
	}
}

func lifecycleSocket(t *testing.T, h *scenarioHarness) *websocket.Conn {
	t.Helper()
	socket, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { socket.Close() })
	return socket
}

func readLifecycleEnd(t *testing.T, socket *websocket.Conn, runID string) contract.SimulationEndedFrame {
	t.Helper()
	for {
		kind, data := readLifecycleMessage(t, socket)
		if kind == "simulationSnapshot" {
			continue
		}
		var ended contract.SimulationEndedFrame
		decodeLifecycleMessage(t, data, &ended)
		if kind != "simulationEnded" || ended.RunID != runID {
			t.Fatalf("unexpected lifecycle message: %s", data)
		}
		return ended
	}
}

func assertLifecycleMeasurement(t *testing.T, ended contract.SimulationEndedFrame, reason contract.SimulationEndReason) {
	t.Helper()
	if ended.Type != "simulationEnded" || ended.Reason != reason || ended.FinalSnapshot == nil {
		t.Fatalf("missing terminal measurement: %+v", ended)
	}
	if ended.FinalSnapshot.Simulation.RunID != ended.RunID || ended.FinalSnapshot.Simulation.PublicationSequence == "" {
		t.Fatal("final measurement lost its run identity or publication sequence")
	}
	var definition contract.TopologyDocument
	decodeLifecycleMessage(t, ended.TopologyDefinition, &definition)
	assertTelemetryReferences(t, *ended.FinalSnapshot, definition)
}

func readLifecycleReply(t *testing.T, socket *websocket.Conn, commandID string, allowEnd bool) (contract.CommandResultFrame, *contract.SimulationEndedFrame) {
	t.Helper()
	var ended *contract.SimulationEndedFrame
	for {
		kind, data := readLifecycleMessage(t, socket)
		switch kind {
		case "simulationSnapshot":
			// Idle is replaceable telemetry and can arrive on either side of the reply.
		case "simulationEnded":
			if !allowEnd || ended != nil {
				t.Fatalf("unexpected duplicate terminal outcome: %s", data)
			}
			ended = &contract.SimulationEndedFrame{}
			decodeLifecycleMessage(t, data, ended)
		case "commandResult":
			var result contract.CommandResultFrame
			decodeLifecycleMessage(t, data, &result)
			if result.CommandID != commandID || result.CommandType != contract.StopSimulation {
				t.Fatalf("unexpected private reply: %s", data)
			}
			return result, ended
		default:
			t.Fatalf("unexpected lifecycle message: %s", data)
		}
	}
}

func assertLifecycleObservation(t *testing.T, ended contract.SimulationEndedFrame, result contract.CommandResultFrame) {
	t.Helper()
	if result.Status != contract.CommandApplied || result.Error != "" || result.RunID != ended.RunID ||
		!reflect.DeepEqual(result.FinalSnapshot, ended.FinalSnapshot) || !reflect.DeepEqual(result.TopologyDefinition, ended.TopologyDefinition) {
		t.Fatalf("Stop reply did not preserve the exact terminal observation: %+v", result)
	}
}

func TestLifecycleFastRunsPreserveUnreadFinalOutcomes(t *testing.T) {
	h := newScenarioHarness(t)
	witness := lifecycleSocket(t, h)
	input := h.defaults.scenarioDefinition()
	input.Execution.Speed, input.Execution.Duration = 0, 100*time.Millisecond
	input.Workload.Defaults.Bytes, input.Workload.ClientGroups[0].Count = 0, 1
	first := startTelemetryScenario(t, h, input)
	firstEnd := readLifecycleEnd(t, witness, first.RunID)
	input.Workload.ClientGroups[0].Count = 2
	second := startTelemetryScenario(t, h, input)
	secondEnd := readLifecycleEnd(t, witness, second.RunID)
	if topologyDocument(t, h).RunID != second.RunID {
		t.Fatal("HTTP definitions did not advance to the second run")
	}

	// This browser has read nothing while both complete runs and HTTP definitions advance.
	sawFirst := false
	for {
		kind, data := readLifecycleMessage(t, h.socket)
		if kind == "simulationSnapshot" {
			var snapshot contract.SimulationSnapshot
			decodeLifecycleMessage(t, data, &snapshot)
			if snapshot.Simulation.RunID == second.RunID && !sawFirst {
				t.Fatal("second run telemetry overtook the first terminal outcome")
			}
			continue
		}
		var ended contract.SimulationEndedFrame
		decodeLifecycleMessage(t, data, &ended)
		assertLifecycleMeasurement(t, ended, contract.SimulationFinished)
		if ended.CommandID != "" || ended.FinalSnapshot.Simulation.Status != "finished" {
			t.Fatal("natural completion has an incorrect status or Stop command identity")
		}
		if ended.RunID == first.RunID && !sawFirst {
			sawFirst = true
			if !reflect.DeepEqual(ended, firstEnd) {
				t.Fatal("unread first outcome changed after the next run started")
			}
			continue
		}
		if ended.RunID != second.RunID || !sawFirst || !reflect.DeepEqual(ended, secondEnd) {
			t.Fatalf("terminal outcomes were lost, duplicated, or reordered: %s", data)
		}
		break
	}
	if reconnected := readLifecycleEnd(t, lifecycleSocket(t, h), second.RunID); !reflect.DeepEqual(reconnected, secondEnd) {
		t.Fatal("reconnect changed the retained final outcome")
	}
}

func TestLifecycleRunningStopPublishesBeforePrivateReply(t *testing.T) {
	h := newScenarioHarness(t)
	other := lifecycleSocket(t, h)
	input := h.defaults.scenarioDefinition()
	input.Execution.Duration, input.Execution.Speed, input.Execution.SnapshotInterval = time.Hour, 1, time.Millisecond
	input.Workload.Defaults.Bytes = 1 << 30
	input.Workload.Defaults.PacketGap = 10 * time.Millisecond
	input.Workload.ClientGroups[0].Count = 1
	receipt := startTelemetryScenario(t, h, input)
	var active contract.SimulationSnapshot
	for {
		kind, data := readLifecycleMessage(t, h.socket)
		if kind != "simulationSnapshot" {
			t.Fatalf("run ended before Stop was submitted: %s", data)
		}
		decodeLifecycleMessage(t, data, &active)
		if active.Simulation.RunID == receipt.RunID && active.Simulation.Performance != nil && active.Simulation.Performance.EventsExecuted > 0 {
			break
		}
	}
	if err := h.socket.WriteJSON(contract.Envelope{Type: contract.StopSimulation, RunID: receipt.RunID, CommandID: "stop-running"}); err != nil {
		t.Fatal(err)
	}
	result, ended := readLifecycleReply(t, h.socket, "stop-running", true)
	if ended == nil {
		t.Fatal("private applied reply overtook the terminal outcome")
	}
	assertLifecycleMeasurement(t, *ended, contract.SimulationStopped)
	assertLifecycleObservation(t, *ended, result)
	if ended.CommandID != "stop-running" || ended.FinalSnapshot.Simulation.Performance.EventsExecuted < active.Simulation.Performance.EventsExecuted ||
		ended.FinalSnapshot.Simulation.Transfers.InProgress != 1 || len(ended.FinalSnapshot.Topology.Nodes) == 0 {
		t.Fatal("Stop captured metrics after clearing the live model")
	}
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current/topology", nil, http.StatusNotFound)
	if broadcast := readLifecycleEnd(t, other, receipt.RunID); !reflect.DeepEqual(broadcast, *ended) {
		t.Fatal("connections received different terminal observations")
	}
	// The next reply proves there was no duplicate outcome or leaked private applied reply.
	for index, socket := range []*websocket.Conn{h.socket, other} {
		commandID := []string{"barrier-owner", "barrier-other"}[index]
		if err := socket.WriteJSON(contract.Envelope{Type: contract.StopSimulation, RunID: receipt.RunID, CommandID: commandID}); err != nil {
			t.Fatal(err)
		}
		if reply, _ := readLifecycleReply(t, socket, commandID, false); reply.Status != contract.CommandRejected {
			t.Fatal("cleared run accepted a later Stop")
		}
	}
}
