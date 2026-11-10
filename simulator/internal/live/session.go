package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
	"load-balancer/internal/execution"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
)

// runInteractive owns lifecycle; projection and publication stay outside World.
// Every observation still executes synchronously on the model's owner goroutine.
func runInteractive(
	ctx context.Context,
	initial contract.SimulationConfig,
	hub *httpserver.Hub,
	control *runController,
	publication *httpserver.Publication,
) error {
	var current *simulation.World
	defer func() {
		if current != nil {
			current.Stop()
		}
	}()
	var started time.Time
	var elapsed time.Duration
	publicationState := &runPublication{hub: hub, topology: publication}
	config := initial
	status, lastError, runID := "idle", "", ""

	runState := func() presentation.RunState {
		if current != nil {
			config.Algorithm = current.Info().Algorithm
		}
		return presentation.RunState{
			Status:    status,
			Algorithm: config.Algorithm,
			Error:     lastError,
			Elapsed:   elapsed,
		}
	}
	publish := func() error {
		return publicationState.publish(current, runState())
	}
	if err := publish(); err != nil {
		return err
	}

	clear := func(request *stopRequest) (stopObservation, error) {
		var observation stopObservation
		var endError error
		if current != nil && runID == request.runID {
			if publicationState.ended == nil {
				current.SampleTraffic()
				elapsed = time.Since(started)
			}
			observation, endError = publicationState.end(
				current, runState(), contract.SimulationStopped, request.commandID,
			)
		} else {
			// A reserved Start can be stopped before it has a model or measurement.
			endError = hub.BroadcastEnded(contract.SimulationEndedFrame{
				Type: "simulationEnded", RunID: request.runID,
				Reason: contract.SimulationStopped, CommandID: request.commandID,
			})
		}
		// Whole-run Stop is allowed to clear all endpoint and network state together.
		if current != nil {
			current.Stop()
		}
		current = nil
		publicationState = &runPublication{hub: hub, topology: publication}
		started, elapsed = time.Time{}, 0
		status, lastError, runID = "idle", "", ""
		err := publish()
		log.Print("Simulation stopped and cleared; waiting for another start")
		return observation, errors.Join(endError, err)
	}
	applyStop := func(request *stopRequest) error {
		return control.applyStop(request, func() (stopObservation, error) {
			return clear(request)
		})
	}
	stopCancelledRun := func() error {
		select {
		case request := <-control.stops:
			return applyStop(request)
		case <-ctx.Done():
			return nil
		}
	}

	for {
		var request startRequest
		select {
		case <-ctx.Done():
			return nil
		case stop := <-control.stops:
			if err := applyStop(stop); err != nil {
				return err
			}
			continue
		case request = <-control.starts:
		}
		runCtx := control.contextForRun()
		if runCtx.Err() != nil {
			if err := stopCancelledRun(); err != nil {
				return err
			}
			continue
		}
		prepared, err := prepareStart(request)
		if err != nil {
			control.finishRun()
			if request.command != nil && request.command.reply != nil {
				// Reject this Start privately; the previous run's final sample stays frozen.
				request.command.reply(contract.CommandResultFrame{
					Type:        "commandResult",
					CommandType: contract.StartSimulation,
					RunID:       request.runID,
					CommandID:   request.command.commandID,
					Status:      contract.CommandRejected,
					Error:       fmt.Sprintf("invalid configuration: %v", err),
				})
			}
			continue
		}
		if runCtx.Err() != nil {
			if err := stopCancelledRun(); err != nil {
				return err
			}
			continue
		}

		config, status, lastError = prepared.config, "running", ""
		runID = request.runID
		if prepared.document != nil {
			request.experiment = execution.Context(prepared.document, contract.ExperimentExecution{
				Speed: prepared.plan.Fixed().Execution.Speed,
			})
			control.rememberRun(runID, prepared.document, request.experiment)
		} else {
			control.setRunStatus(runID, status)
		}
		started, elapsed = time.Now(), 0
		if current != nil {
			current.Stop()
		}
		current, err = prepared.newWorld()
		if err != nil {
			control.setRunStatus(runID, "failed")
			control.finishRun()
			return err
		}
		publicationState = &runPublication{
			hub: hub, topology: publication, projector: presentation.NewProjector(runID),
			runID: runID, experiment: &request.experiment, started: started,
		}
		if err := publish(); err != nil {
			return err
		}
		log.Printf("Starting simulation: %+v", config)
		err = execution.RunWorld(runCtx, current, publish)
		elapsed = time.Since(started)
		if ctx.Err() != nil {
			return nil
		}
		if runCtx.Err() != nil {
			if err := stopCancelledRun(); err != nil {
				return err
			}
			continue
		}
		if encodeErr := json.NewEncoder(os.Stdout).Encode(presentation.BuildResult(current, elapsed)); encodeErr != nil {
			return encodeErr
		}
		nextStatus, nextError := "finished", ""
		reason := contract.SimulationFinished
		if err != nil {
			nextStatus, nextError = "failed", err.Error()
			reason = contract.SimulationFailed
		}
		finished, err := control.finishNaturally(runID, nextStatus, func() error {
			status, lastError = nextStatus, nextError
			_, err := publicationState.end(current, runState(), reason, "")
			return err
		})
		if err != nil {
			return err
		}
		if !finished {
			if err := stopCancelledRun(); err != nil {
				return err
			}
			continue
		}
		log.Printf("Simulation %s; waiting for another start", status)
	}
}
