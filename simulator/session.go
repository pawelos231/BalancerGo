package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	httpserver "load-balancer/http"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
)

// runInteractive owns lifecycle; projection and publication stay outside World.
// Every observation still executes synchronously on the model's owner goroutine.
func runInteractive(
	ctx context.Context,
	defaults configuration,
	hub *httpserver.Hub,
	control *runController,
	publication *httpserver.Publication,
) error {
	var current *simulation.World
	var projector *presentation.Projector
	var started time.Time
	var elapsed time.Duration
	config := defaults.publicConfig()
	status, lastError, runID := "idle", "", ""

	publish := func() error {
		if current != nil {
			config.Algorithm = current.Info().Algorithm
		}
		frame, definition, err := presentation.BuildSnapshot(current, projector, presentation.RunState{
			Status:    status,
			RunID:     runID,
			Algorithm: config.Algorithm,
			Error:     lastError,
			Elapsed:   elapsed,
		})
		if err != nil {
			return err
		}
		if current == nil {
			publication.ClearTopology()
		} else if definition != nil {
			if err := publication.PublishTopology(definition); err != nil {
				return err
			}
		}

		// Running measurements include projection and definition encoding, as before.
		if current != nil && status == "running" {
			counters := frame.Simulation.Performance
			frame.Simulation.Performance = presentation.MeasurePerformance(
				time.Since(started),
				counters.EventsExecuted,
				counters.ForwardedPackets,
			)
		}
		return hub.BroadcastState(frame)
	}
	if err := publish(); err != nil {
		return err
	}

	clear := func() error {
		// Stop discards the model rather than closing each connection separately.
		current, projector = nil, nil
		started, elapsed = time.Time{}, 0
		status, lastError, runID = "idle", "", ""
		control.forgetRun()
		control.finishRun()
		err := control.acknowledgeStop(publish)
		log.Print("Simulation stopped and cleared; waiting for another start")
		return err
	}

	for {
		var request startRequest
		select {
		case <-ctx.Done():
			return nil
		case <-control.stops:
			if err := clear(); err != nil {
				return err
			}
			continue
		case request = <-control.starts:
		}
		runCtx := control.contextForRun()
		if runCtx.Err() != nil {
			if err := clear(); err != nil {
				return err
			}
			continue
		}
		prepared, err := prepareStart(request, defaults)
		if err != nil {
			lastError = fmt.Sprintf("invalid configuration: %v", err)
			control.finishRun()
			if err := publish(); err != nil {
				return err
			}
			continue
		}
		if runCtx.Err() != nil {
			if err := clear(); err != nil {
				return err
			}
			continue
		}

		config, status, lastError = prepared.config, "running", ""
		runID = request.runID
		if prepared.document != nil {
			control.rememberRun(runID, prepared.document)
		} else {
			control.setRunStatus(runID, status)
		}
		started, elapsed = time.Now(), 0
		current = simulation.New(prepared.scenario)
		projector = presentation.NewProjector(runID)
		handled := func(err error) {
			lastError = ""
			if err != nil {
				lastError = err.Error()
			}
			if err := publish(); err != nil {
				log.Printf("snapshot: %v", err)
			}
		}
		if err := publish(); err != nil {
			return err
		}
		log.Printf("Starting simulation: %+v", config)
		err = runWorld(runCtx, current, control.commands, publish, handled)
		elapsed = time.Since(started)
		if ctx.Err() != nil {
			return nil
		}
		if runCtx.Err() != nil {
			if err := clear(); err != nil {
				return err
			}
			continue
		}
		if encodeErr := json.NewEncoder(os.Stdout).Encode(presentation.BuildResult(current, elapsed)); encodeErr != nil {
			return encodeErr
		}
		status = "finished"
		if err != nil {
			status, lastError = "failed", err.Error()
		}
		control.setRunStatus(runID, status)
		control.finishRun()
		if err := publish(); err != nil {
			return err
		}
		log.Printf("Simulation %s; waiting for another start", status)
	}
}
