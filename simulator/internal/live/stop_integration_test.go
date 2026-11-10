package live

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"load-balancer/internal/contract"

	"github.com/gorilla/websocket"
)

func readStopResult(
	t *testing.T,
	socket *websocket.Conn,
	runID, commandID string,
	status contract.CommandResultStatus,
) {
	t.Helper()
	if err := socket.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		_, data, err := socket.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var result contract.CommandResultFrame
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if result.Type == "simulationSnapshot" || result.Type == "simulationEnded" {
			continue
		}
		if result.Type != "commandResult" || result.CommandType != contract.StopSimulation ||
			result.RunID != runID || result.CommandID != commandID || result.Status != status {
			t.Fatalf("unexpected command reply on this connection: %s", data)
		}
		if (status == contract.CommandRejected) != (result.Error != "") {
			t.Fatalf("command result has inconsistent error: %s", data)
		}
		return
	}
}

func TestStopResultsStayOnSubmittingWebSocket(t *testing.T) {
	h := newScenarioHarness(t)
	url := "ws" + strings.TrimPrefix(h.server.URL, "http") + "/ws"
	other, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	input := h.defaults()
	input.Fixed.Execution.Duration = time.Hour
	input.Fixed.Execution.Speed = 1
	input.Fixed.Execution.SnapshotInterval = time.Hour
	{
		initialState := input.States["0"]
		initialState.Workload.ClientGroups[0].Count = 0
		input.States["0"] = initialState
	}
	receipt := startTelemetryScenario(t, h, input)
	for _, socket := range []*websocket.Conn{h.socket, other} {
		readScenarioSnapshot(t, socket, func(snapshot contract.SimulationSnapshot) bool {
			return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "running"
		})
	}

	if err := other.WriteJSON(contract.Envelope{
		Type: contract.StopSimulation, RunID: "stale-run", CommandID: "reject-other",
	}); err != nil {
		t.Fatal(err)
	}
	readStopResult(t, other, "stale-run", "reject-other", contract.CommandRejected)
	if h.control.contextForRun().Err() != nil {
		t.Fatal("rejected WebSocket Stop canceled the active run")
	}
	if err := h.socket.WriteJSON(contract.Envelope{
		Type: contract.StopSimulation, RunID: receipt.RunID, CommandID: "apply-owner",
	}); err != nil {
		t.Fatal(err)
	}
	readStopResult(t, h.socket, receipt.RunID, "apply-owner", contract.CommandApplied)
	// An applied result certifies that both retained configuration and topology are already gone.
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current/topology", nil, http.StatusNotFound)

	// Each later reply is a per-connection barrier: any leaked result must be observed first.
	for index, socket := range []*websocket.Conn{other, h.socket} {
		commandID := []string{"barrier-other", "barrier-owner"}[index]
		if err := socket.WriteJSON(contract.Envelope{
			Type: contract.StopSimulation, RunID: receipt.RunID, CommandID: commandID,
		}); err != nil {
			t.Fatal(err)
		}
		readStopResult(t, socket, receipt.RunID, commandID, contract.CommandRejected)
	}
}
