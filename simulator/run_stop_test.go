package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"load-balancer/internal/contract"
)

func reserveStopTestRun(t *testing.T, control *runController) string {
	t.Helper()
	if err := control.submit(context.Background(), runtimeCommand{kind: startCommand}); err != nil {
		t.Fatal(err)
	}
	return control.reservedRunID
}

func submitStopTestCommand(
	control *runController,
	runID, commandID string,
	reply func(contract.CommandResultFrame),
) error {
	return submitFrontendCommand(control, context.Background(), contract.Envelope{
		Type: contract.StopSimulation, RunID: runID, CommandID: commandID,
	}, reply)
}

func TestStopRequiresRunAndCommandIDs(t *testing.T) {
	control := newRunController(context.Background())
	runID := reserveStopTestRun(t, control)
	for _, input := range []struct{ runID, commandID string }{
		{"", "stop-1"}, {" \t", "stop-1"}, {runID, ""}, {runID, " \t"},
	} {
		if err := submitStopTestCommand(control, input.runID, input.commandID, nil); err == nil {
			t.Errorf("Stop accepted missing identity: %+v", input)
		}
	}
	if control.contextForRun().Err() != nil || len(control.stops) != 0 || control.pendingStop != nil {
		t.Fatal("invalid Stop canceled or queued work for the reserved run")
	}
}

func TestStaleStopCannotCancelReservedRun(t *testing.T) {
	for _, websocketStart := range []bool{false, true} {
		name := "scenario"
		if websocketStart {
			name = "websocket_with_previous_record"
		}
		t.Run(name, func(t *testing.T) {
			control := newRunController(context.Background())
			control.current = &runRecord{RunID: "run-A", Status: "finished"}
			var runID string
			if websocketStart {
				runID = reserveStopTestRun(t, control)
				if control.currentRun().RunID != "run-A" {
					t.Fatal("WebSocket start unexpectedly replaced the previous record")
				}
			} else {
				receipt, err := control.startTimeline(context.Background(), []byte(`{}`), mustCompileScenario(testConfiguration().scenarioDefinitionV2()))
				if err != nil {
					t.Fatal(err)
				}
				runID = receipt.RunID
			}
			if err := submitStopTestCommand(control, "run-A", "stale-stop", nil); err == nil {
				t.Fatal("Stop for run A was admitted while run B was reserved")
			}
			if control.contextForRun().Err() != nil || control.reservedRunID != runID || len(control.stops) != 0 {
				t.Fatal("stale Stop changed the newly reserved run")
			}
		})
	}
}

func TestStopBeforeModelCreationSurvivesNormalFinish(t *testing.T) {
	for _, finishFirst := range []bool{false, true} {
		name := "pending_start"
		if finishFirst {
			name = "normal_finish_after_stop_admission"
		}
		t.Run(name, func(t *testing.T) {
			control := newRunController(context.Background())
			runID := reserveStopTestRun(t, control)
			runCtx := control.contextForRun()
			cleared, replies := false, 0
			err := submitStopTestCommand(control, runID, "stop-first", func(result contract.CommandResultFrame) {
				replies++
				if !cleared || control.currentRun() != nil || control.contextForRun() != nil {
					t.Error("Stop replied before the owner finished clearing the run")
				}
				err := control.submit(context.Background(), runtimeCommand{kind: startCommand})
				if !errors.Is(err, errSimulationBusy) {
					t.Errorf("Start bypassed the Stop result barrier: %v", err)
				}
				if result.Type != "commandResult" || result.CommandType != contract.StopSimulation ||
					result.RunID != runID || result.CommandID != "stop-first" ||
					result.Status != contract.CommandApplied || result.Error != "" {
					t.Errorf("incorrect Stop result: %+v", result)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if !errors.Is(runCtx.Err(), context.Canceled) || replies != 0 {
				t.Fatal("Stop did not cancel immediately and defer its reply")
			}
			err = submitStopTestCommand(control, runID, "stop-duplicate", func(contract.CommandResultFrame) {
				t.Error("duplicate Stop received an owner callback")
			})
			if err == nil || !strings.Contains(err.Error(), "already pending") || len(control.stops) != 1 {
				t.Fatalf("duplicate Stop was not explicitly rejected: %v", err)
			}
			if finishFirst {
				control.finishRun()
			}
			err = control.submit(context.Background(), runtimeCommand{kind: startCommand})
			if !errors.Is(err, errSimulationBusy) {
				t.Fatalf("Start bypassed pending Stop: %v", err)
			}
			if err := control.applyStop(<-control.stops, func() (stopObservation, error) {
				cleared = true
				return stopObservation{}, nil
			}); err != nil {
				t.Fatal(err)
			}
			if replies != 1 || len(control.starts) != 0 || control.pendingStop != nil || control.reservedRunID != "" {
				t.Fatal("Stop did not clear the pending start and release admission")
			}
			reserveStopTestRun(t, control)
		})
	}
}

func TestStopClearsRetainedFinishedRun(t *testing.T) {
	control := newRunController(context.Background())
	control.current = &runRecord{RunID: "run-finished", Status: "finished", Scenario: []byte(`{}`)}
	cleared, replied := false, false
	err := submitStopTestCommand(control, "run-finished", "stop-finished", func(result contract.CommandResultFrame) {
		replied = true
		if !cleared || control.currentRun() != nil || result.Status != contract.CommandApplied {
			t.Errorf("finished Stop replied before clearing: %+v", result)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := control.applyStop(<-control.stops, func() (stopObservation, error) {
		cleared = true
		return stopObservation{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !replied {
		t.Fatal("finished Stop did not return its result")
	}
}

func TestStopRechecksTargetBeforeClearing(t *testing.T) {
	control := newRunController(context.Background())
	runID := reserveStopTestRun(t, control)
	var result contract.CommandResultFrame
	if err := submitStopTestCommand(control, runID, "stop-old-target", func(reply contract.CommandResultFrame) {
		result = reply
	}); err != nil {
		t.Fatal(err)
	}
	request := <-control.stops
	// Force an owner-side target change to exercise the execution guard independently of admission.
	control.current = &runRecord{RunID: "run-B", Status: "running"}
	control.reservedRunID = "run-B"
	control.runCtx, control.cancelRun = context.WithCancel(context.Background())
	defer control.cancelRun()
	cleared := false
	if err := control.applyStop(request, func() (stopObservation, error) {
		cleared = true
		return stopObservation{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if cleared || control.currentRun().RunID != "run-B" || control.contextForRun().Err() != nil {
		t.Fatal("old Stop cleared or canceled the changed target")
	}
	if result.RunID != runID || result.CommandID != "stop-old-target" ||
		result.Status != contract.CommandRejected || result.Error == "" || control.pendingStop != nil {
		t.Fatalf("execution guard did not reject the original command: %+v", result)
	}
}
