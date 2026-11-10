// Package app reads CLI settings and starts interactive or headless experiments.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"runtime/trace"
	"syscall"
	"time"

	"load-balancer/internal/execution"
	"load-balancer/internal/live"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
)

// Run selects the execution mode from CLI flags and owns process cancellation.
func Run() error {
	defaults, prepared, err := readConfiguration()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Full JSON scenarios use a standalone run and never start the HTTP service.
	if defaults.scenarioPath != "" {
		if defaults.seeds != "" {
			return runScenarioSeries(ctx, defaults, os.Stdout)
		}
		return runScenarioFile(ctx, defaults, os.Stdout)
	}

	if defaults.tracePath != "" {
		file, err := os.Create(defaults.tracePath)
		if err != nil {
			return err
		}
		if err := trace.Start(file); err != nil {
			file.Close()
			return err
		}
		defer file.Close()
		defer trace.Stop()
	}

	// Headless runs remain explicit CLI experiments without a browser.
	if defaults.headless {
		sim, err := simulation.NewTimeline(prepared.plan)
		if err != nil {
			return err
		}
		started := time.Now()
		err = execution.RunUnpacedWorld(ctx, sim)
		result := presentation.BuildResult(sim, time.Since(started))
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			return encodeErr
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	return live.Serve(ctx, stop, defaults.scenarioDefinition, defaults.publicConfig())
}
