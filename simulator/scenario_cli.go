package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/trace"
	"strconv"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

const (
	experimentReportVersion = 1
	experimentModel         = "tcp-reno-foundation-v1"
)

// loadedScenario keeps the importable input beside its checked runtime settings.
// A series changes only the seed on value copies of these settings.
type loadedScenario struct {
	document   []byte
	definition scenario.Scenario
	resolved   scenario.Normalized
}

// runScenarioFile runs the same full document accepted by the frontend.
// The report preserves its input; only the runtime copy disables pacing.
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
		loaded.resolved,
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
	resolved scenario.Normalized,
	traced bool,
) (contract.ExperimentReport, error) {
	resolved.Execution.Speed = 0
	world := simulation.New(resolved)

	// Match the existing headless clock: setup and report encoding are outside it.
	wallStart := time.Now()
	runErr := runWorld(ctx, world, nil, nil, nil)
	elapsed := time.Since(wallStart)

	report := contract.ExperimentReport{
		Version:  experimentReportVersion,
		Status:   "finished",
		Manifest: experimentManifest(document, traced),
		Result:   presentation.BuildResult(world, elapsed),
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
	resolved, err := scenario.Normalize(definition)
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
		resolved:   resolved,
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

// experimentManifest identifies the canonical input and records runtime overrides.
func experimentManifest(document []byte, traced bool) contract.ExperimentManifest {
	hash := sha256.Sum256(document)
	return contract.ExperimentManifest{
		Model:          experimentModel,
		Scenario:       document,
		ScenarioSHA256: hex.EncodeToString(hash[:]),
		Execution: contract.ExperimentExecution{
			Headless:     true,
			Speed:        0,
			TraceEnabled: traced,
		},
		Build: experimentBuild(),
	}
}

// experimentBuild keeps unknown VCS information absent rather than guessing it.
func experimentBuild() contract.ExperimentBuild {
	build := contract.ExperimentBuild{
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return build
	}
	build.ModuleVersion = info.Main.Version
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			build.VCSRevision = setting.Value
		case "vcs.modified":
			modified, err := strconv.ParseBool(setting.Value)
			if err == nil {
				build.VCSModified = &modified
			}
		}
	}
	return build
}
