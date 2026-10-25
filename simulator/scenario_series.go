package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

const experimentSeriesVersion = 1

// runScenarioSeries runs seeds in the requested order, with a fresh world each time.
// Reports stay on disk; the index keeps only their paths, seeds and outcomes.
func runScenarioSeries(
	ctx context.Context,
	options configuration,
	stdout io.Writer,
) error {
	seeds, err := parseSeriesSeeds(options.seeds)
	if err != nil {
		return err
	}
	loaded, err := loadScenarioFile(options.scenarioPath)
	if err != nil {
		return err
	}

	// A new directory prevents overwriting another experiment's results.
	directory := options.outputDirectory
	if err := os.Mkdir(directory, 0755); err != nil {
		return fmt.Errorf("create series directory: %w", err)
	}
	index := newSeriesIndex(loaded.document, seeds)
	if err := writeSeriesIndex(directory, index); err != nil {
		return errors.Join(err, os.Remove(directory))
	}

	var firstFailure error
	for position, seed := range seeds {
		if err := ctx.Err(); err != nil {
			return finishScenarioSeries(directory, index, stdout, err)
		}
		entry := &index.Runs[position]
		entry.Status = "running"
		if err := writeSeriesIndex(directory, index); err != nil {
			entry.Status, entry.Error = "failed", err.Error()
			return finishScenarioSeries(directory, index, stdout, err)
		}

		// Only the seed changes. The base input and its requested pacing stay intact.
		definition := loaded.definition
		definition.Execution.Seed = seed
		document, err := scenario.EncodeDocument(definition)
		if err != nil {
			entry.Status, entry.Error = "failed", err.Error()
			return finishScenarioSeries(directory, index, stdout, err)
		}
		resolved := loaded.resolved
		resolved.Execution.Seed = seed
		report, runErr := executeScenario(ctx, document, resolved, false)

		filename := entry.ID + ".json"
		if err := writeSeriesRun(filepath.Join(directory, filename), report); err != nil {
			entry.Status, entry.Error = "failed", err.Error()
			return finishScenarioSeries(directory, index, stdout, err)
		}
		entry.Report = filename
		entry.Status = report.Status
		entry.Error = report.Error

		// Cancellation stops future runs. A model failure, such as the event limit,
		// is retained in its report and does not prevent trying the next seed.
		if isCancellation(runErr) {
			return finishScenarioSeries(directory, index, stdout, runErr)
		}
		if runErr != nil && firstFailure == nil {
			firstFailure = runErr
		}
		if err := writeSeriesIndex(directory, index); err != nil {
			return finishScenarioSeries(directory, index, stdout, err)
		}
	}
	return finishScenarioSeries(directory, index, stdout, firstFailure)
}

// parseSeriesSeeds preserves order and duplicates, including the full int64 range.
// Repeated seeds are useful for replay checks; they are not independent samples.
func parseSeriesSeeds(input string) ([]int64, error) {
	if strings.TrimSpace(input) == "" {
		return nil, errors.New("-seeds requires a nonempty seed list")
	}
	parts := strings.Split(input, ",")
	seeds := make([]int64, 0, len(parts))
	for index, part := range parts {
		seed, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("-seeds: item %d must be a signed decimal int64", index+1)
		}
		seeds = append(seeds, seed)
	}
	return seeds, nil
}

// newSeriesIndex reserves an entry for every planned run, including future ones.
func newSeriesIndex(document []byte, seeds []int64) *contract.ExperimentSeriesReport {
	index := &contract.ExperimentSeriesReport{
		Version:  experimentSeriesVersion,
		Status:   "running",
		Manifest: experimentManifest(document, false),
		Runs:     make([]contract.ExperimentSeriesRun, len(seeds)),
	}
	for position, seed := range seeds {
		index.Runs[position] = contract.ExperimentSeriesRun{
			ID:     fmt.Sprintf("run-%04d", position+1),
			Seed:   strconv.FormatInt(seed, 10),
			Status: "notStarted",
		}
	}
	return index
}

// writeSeriesRun claims a new filename and checks both encoding and file close.
// The caller adds its path to the index only after this succeeds.
func writeSeriesRun(path string, report contract.ExperimentReport) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create run report: %w", err)
	}
	writeErr := writeCLIJSON(file, report)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write run report: %w", err)
	}
	return nil
}

// writeSeriesIndex replaces the previous checkpoint only after encoding succeeds.
// Temporary files live beside the index so the replacement stays on one filesystem.
func writeSeriesIndex(directory string, index *contract.ExperimentSeriesReport) error {
	file, err := os.CreateTemp(directory, ".index-*.json")
	if err != nil {
		return fmt.Errorf("create series index: %w", err)
	}
	defer os.Remove(file.Name())

	writeErr := writeCLIJSON(file, index)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write series index: %w", err)
	}
	path := filepath.Join(directory, "index.json")
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace series index: %w", err)
	}
	return nil
}

// finishScenarioSeries saves the final index before returning it to CLI consumers.
// A late cancellation cannot change already completed runs into cancelled ones.
func finishScenarioSeries(
	directory string,
	index *contract.ExperimentSeriesReport,
	stdout io.Writer,
	runErr error,
) error {
	index.Status = "finished"
	if runErr != nil {
		index.Status = "failed"
		index.Error = runErr.Error()
		if isCancellation(runErr) {
			index.Status = "cancelled"
		}
	}
	if err := writeSeriesIndex(directory, index); err != nil {
		return errors.Join(runErr, err)
	}
	if err := writeCLIJSON(stdout, index); err != nil {
		return errors.Join(runErr, fmt.Errorf("write series index to stdout: %w", err))
	}
	return runErr
}
