package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"load-balancer/internal/contract"
)

func TestLifecycleFinishedResetKeepsOriginalObservation(t *testing.T) {
	h := newScenarioHarness(t)
	input := h.defaults.scenarioDefinition()
	input.Fixed.Execution.Speed, input.Fixed.Execution.Duration = 0, 100*time.Millisecond
	input.Fixed.Workload.Defaults.Bytes = 0
	receipt := startTelemetryScenario(t, h, input)
	ended := readLifecycleEnd(t, h.socket, receipt.RunID)
	assertLifecycleMeasurement(t, ended, contract.SimulationFinished)
	if err := h.socket.WriteJSON(contract.Envelope{Type: contract.StopSimulation, RunID: receipt.RunID, CommandID: "reset-finished"}); err != nil {
		t.Fatal(err)
	}
	result, _ := readLifecycleReply(t, h.socket, "reset-finished", false)
	assertLifecycleObservation(t, ended, result)
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current/topology", nil, http.StatusNotFound)
	// The later rejected reply is a FIFO barrier for any accidentally queued second end.
	if err := h.socket.WriteJSON(contract.Envelope{Type: contract.StopSimulation, RunID: receipt.RunID, CommandID: "reset-barrier"}); err != nil {
		t.Fatal(err)
	}
	if reply, _ := readLifecycleReply(t, h.socket, "reset-barrier", false); reply.Status != contract.CommandRejected {
		t.Fatal("finished reset did not clear its target")
	}
}

func TestLifecycleFailedRunPreservesFailureMeasurement(t *testing.T) {
	h := newScenarioHarness(t)
	input := h.defaults.scenarioDefinition()
	input.Fixed.Execution.Speed, input.Fixed.Execution.MaxEvents = 0, 1
	{
		initialState := input.States["0"]
		initialState.Workload.ClientGroups[0].Count = 1
		input.States["0"] = initialState
	}
	receipt := startTelemetryScenario(t, h, input)
	ended := readLifecycleEnd(t, h.socket, receipt.RunID)
	assertLifecycleMeasurement(t, ended, contract.SimulationFailed)
	if ended.CommandID != "" || ended.FinalSnapshot.Simulation.Status != "failed" ||
		ended.FinalSnapshot.Simulation.Error == "" || ended.FinalSnapshot.Simulation.Performance.EventsExecuted != 1 {
		t.Fatalf("failed outcome lost its error or last counters: %+v", ended.FinalSnapshot)
	}
}

func TestLifecycleStopBeforeCreationHasNoForeignMeasurement(t *testing.T) {
	for _, retainFinished := range []bool{false, true} {
		name := "without_previous_world"
		if retainFinished {
			name = "with_previous_finished_world"
		}
		t.Run(name, func(t *testing.T) {
			h := newScenarioHarness(t)
			var previousRunID string
			if retainFinished {
				input := h.defaults.scenarioDefinition()
				input.Fixed.Execution.Speed, input.Fixed.Execution.Duration = 0, time.Millisecond
				{
					initialState := input.States["0"]
					initialState.Workload.ClientGroups[0].Count = 0
					input.States["0"] = initialState
				}
				previousRunID = startTelemetryScenario(t, h, input).RunID
				readLifecycleEnd(t, h.socket, previousRunID)
			}

			// Queue Start and Stop under the admission lock, before the owner can create B.
			replies := make(chan contract.CommandResultFrame, 1)
			h.control.mu.Lock()
			receipt, err := h.control.enqueueStart(startRequest{command: &runtimeCommand{Payload: []byte(`{}`)}}, nil)
			if err == nil {
				if retainFinished && h.control.current.RunID != previousRunID {
					t.Error("reserved WebSocket Start discarded the previous record before creation")
				}
				err = h.control.enqueueStop(runtimeCommand{
					runID: receipt.RunID, commandID: "stop-reserved",
					reply: func(result contract.CommandResultFrame) { replies <- result },
				})
			}
			h.control.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			for {
				kind, data := readLifecycleMessage(t, h.socket)
				if kind == "simulationSnapshot" {
					var snapshot contract.SimulationSnapshot
					decodeLifecycleMessage(t, data, &snapshot)
					if snapshot.Simulation.RunID == receipt.RunID {
						t.Fatal("stopped reserved run unexpectedly created a model")
					}
					continue
				}
				var ended contract.SimulationEndedFrame
				decodeLifecycleMessage(t, data, &ended)
				var fields map[string]any
				decodeLifecycleMessage(t, data, &fields)
				if _, exists := fields["finalSnapshot"]; !exists {
					t.Fatal("unmeasured outcome omitted finalSnapshot instead of encoding null")
				}
				if _, exists := fields["topologyDefinition"]; !exists {
					t.Fatal("unmeasured outcome omitted topologyDefinition instead of encoding null")
				}
				if kind != "simulationEnded" || ended.RunID != receipt.RunID || ended.Reason != contract.SimulationStopped ||
					ended.CommandID != "stop-reserved" || fields["finalSnapshot"] != nil || fields["topologyDefinition"] != nil {
					t.Fatalf("reserved run inherited another run's measurement: %s", data)
				}
				break
			}
			select {
			case reply := <-replies:
				if reply.Status != contract.CommandApplied || reply.RunID != receipt.RunID ||
					reply.CommandID != "stop-reserved" || reply.FinalSnapshot != nil || reply.TopologyDefinition != nil {
					t.Fatalf("unmeasured Stop returned foreign final data: %+v", reply)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("reserved Stop did not reply")
			}
			scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)
			scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current/topology", nil, http.StatusNotFound)
		})
	}
}

func TestLifecycleNaturalFinishPublishesBeforeAdmissionAndPendingStopWins(t *testing.T) {
	for _, stopPending := range []bool{false, true} {
		name := "natural_completion"
		if stopPending {
			name = "pending_stop"
		}
		t.Run(name, func(t *testing.T) {
			control := newRunController(context.Background())
			runID := reserveStopTestRun(t, control)
			control.current = &runRecord{RunID: runID, Status: "running"}
			if stopPending {
				if err := submitStopTestCommand(control, runID, "stop-wins", nil); err != nil {
					t.Fatal(err)
				}
			}
			published := false
			finished, err := control.finishNaturally(runID, "finished", func() error {
				published = true
				// This callback runs under mu, exactly where the terminal frame is queued.
				_, startErr := control.enqueueStart(startRequest{}, nil)
				if !errors.Is(startErr, errSimulationBusy) {
					t.Errorf("Start was admitted before the terminal publication: %v", startErr)
				}
				return nil
			})
			if err != nil || finished == stopPending || published == stopPending {
				t.Fatalf("natural finish chose the wrong terminal owner: finished=%v published=%v err=%v", finished, published, err)
			}
			if stopPending {
				if control.currentRun().Status != "running" || control.reservedRunID != runID {
					t.Fatal("natural finish changed a run already owned by Stop")
				}
				if err := control.applyStop(<-control.stops, func() (stopObservation, error) { return stopObservation{}, nil }); err != nil {
					t.Fatal(err)
				}
			} else if control.currentRun().Status != "finished" {
				t.Fatal("natural finish did not retain its final lifecycle status")
			}
			reserveStopTestRun(t, control)
		})
	}
}
