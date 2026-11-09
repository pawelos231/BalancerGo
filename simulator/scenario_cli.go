package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/trace"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

// loadedScenario keeps the importable input beside its checked runtime settings.
// A series compiles a fresh plan after changing the seed.
type loadedScenario struct {
	document   []byte
	definition scenario.Scenario
	plan       *scenario.Compiled
}

// runScenarioFile executes the complete scenario document without playback waits.
// The report preserves the admitted source, including its requested speed.
func runScenarioFile(
	ctx context.Context,
	options configuration,
	stdout io.Writer,
) (resultErr error) {
	loaded, err := loadScenarioFile(options.scenarioPath)
	if err != nil {
		return err
	}

	// Reserve a new output before running. Existing inputs and reports stay safe,
	// even when another path names the same file through a link.
	output := stdout
	started := false
	if options.outputPath != "" {
		file, err := os.OpenFile(
			options.outputPath,
			os.O_WRONLY|os.O_CREATE|os.O_EXCL,
			0600,
		)
		if err != nil {
			return fmt.Errorf("create report: %w", err)
		}
		output = file
		defer func() {
			resultErr = errors.Join(resultErr, file.Close())
			if !started {
				resultErr = errors.Join(resultErr, os.Remove(options.outputPath))
			}
		}()
	}

	stopTrace, err := startScenarioTrace(options.tracePath)
	if err != nil {
		return err
	}
	if stopTrace != nil {
		defer func() {
			resultErr = errors.Join(resultErr, stopTrace())
		}()
	}

	started = true
	report, runErr := executeScenario(
		ctx,
		loaded.document,
		loaded.plan,
		options.tracePath != "",
	)

	// Runtime failures still produce a partial report, then a nonzero exit status.
	if err := writeCLIJSON(output, report); err != nil {
		return errors.Join(runErr, fmt.Errorf("write report: %w", err))
	}
	return runErr
}

// executeScenario creates a fresh world for one unpaced experiment.
// Execution errors stay separate from file errors so a series can continue.
func executeScenario(
	ctx context.Context,
	document []byte,
	plan *scenario.Compiled,
	traced bool,
) (contract.ExperimentReport, error) {
	world, err := simulation.NewTimeline(plan)
	if err != nil {
		return contract.ExperimentReport{}, err
	}

	// Match the existing headless clock: setup and report encoding are outside it.
	wallStart := time.Now()
	runErr := runUnpacedWorld(ctx, world)
	elapsed := time.Since(wallStart)

	report := contract.ExperimentReport{
		ReportVersion: contract.CurrentReportVersion,
		Status:        "finished",
		Manifest:      experimentManifest(document, traced),
		Result:        presentation.BuildResult(world, elapsed),
	}
	if runErr != nil {
		report.Status = "failed"
		report.Error = runErr.Error()
		if isCancellation(runErr) {
			report.Status = "cancelled"
		}
	}

	return report, runErr
}

// loadScenarioFile shares strict decoding, validation and exact integer encoding
// with HTTP. The stored document can be imported again without losing profiles.
func loadScenarioFile(path string) (loadedScenario, error) {
	input, err := os.ReadFile(path)
	if err != nil {
		return loadedScenario{}, fmt.Errorf("read scenario: %w", err)
	}
	definition, err := scenario.DecodeDocument(input)
	if err != nil {
		return loadedScenario{}, fmt.Errorf("decode scenario: %w", err)
	}
	plan, err := scenario.Compile(definition)
	if err != nil {
		return loadedScenario{}, fmt.Errorf("validate scenario: %w", err)
	}
	document, err := scenario.EncodeDocument(definition)
	if err != nil {
		return loadedScenario{}, fmt.Errorf("encode scenario: %w", err)
	}
	return loadedScenario{
		document:   document,
		definition: definition,
		plan:       plan,
	}, nil
}

// writeCLIJSON keeps single-run reports and series indexes in the same format.
func writeCLIJSON(output io.Writer, document any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}

// isCancellation distinguishes a request to stop from a model failure.
func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// startScenarioTrace refuses existing files just like report output does.
// Its cleanup stops trace writes before closing the file.
func startScenarioTrace(path string) (func() error, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("create trace: %w", err)
	}
	if err := trace.Start(file); err != nil {
		cleanupErr := errors.Join(file.Close(), os.Remove(path))
		return nil, errors.Join(fmt.Errorf("start trace: %w", err), cleanupErr)
	}
	return func() error {
		trace.Stop()
		return file.Close()
	}, nil
}
