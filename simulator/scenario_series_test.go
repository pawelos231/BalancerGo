package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"load-balancer/internal/contract"
)

// Series tests use the real CLI to check ordered artifacts and fresh worlds.
// Duplicate seeds exercise repeatability independently of different-seed runs.
func TestScenarioSeriesCLI(t *testing.T) {
	executable := buildScenarioCLI(t)

	t.Run("ordered seeds produce independent replayable runs", func(t *testing.T) {
		input := scenarioCLIDefinition()
		inputPath := writeScenarioCLIInput(t, input)
		original, err := os.ReadFile(inputPath)
		if err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(t.TempDir(), "series")
		seeds := []int64{9_007_199_254_740_993, -7, 9_007_199_254_740_993}
		output := executeScenarioCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-seeds", "9007199254740993, -7, 9007199254740993",
			"-output-dir", directory,
		)
		if output.err != nil {
			t.Fatalf("series failed: %v\n%s", output.err, output.stderr)
		}
		index := decodeScenarioSeriesIndex(t, output.stdout, "finished")
		assertScenarioCLIManifest(t, index.Manifest, input, false)
		assertScenarioSeriesIndexSaved(t, directory, output.stdout)
		if len(index.Runs) != len(seeds) {
			t.Fatalf("index contains %d runs, want %d", len(index.Runs), len(seeds))
		}

		results := make([]contract.SimulationResult, 0, len(seeds))
		for position, seed := range seeds {
			run := index.Runs[position]
			id := "run-000" + strconv.Itoa(position+1)
			if run.ID != id || run.Seed != strconv.FormatInt(seed, 10) ||
				run.Status != "finished" || run.Report != id+".json" {
				t.Fatalf("unexpected run %d: %+v", position+1, run)
			}
			report := readScenarioSeriesRun(t, directory, run.Report, "finished")
			requested := input
			requested.Execution.Seed = seed
			assertScenarioCLIManifest(t, report.Manifest, requested, false)

			// Every run starts with zero counters and keeps endpoint overrides.
			if report.Result.Seed != seed || report.Result.CreatedClients != 3 ||
				report.Result.CompletedStreams != 3 || report.Result.ReceivedPayloadBytes != 4131 ||
				report.Result.AcknowledgedPayloadBytes != 4131 {
				t.Fatalf("run %d inherited or lost work: %+v", position+1, report.Result)
			}
			assertScenarioCLILinks(t, report.Result)
			report.Result.WallTime = ""
			results = append(results, report.Result)
		}
		if !reflect.DeepEqual(results[0], results[2]) {
			t.Fatal("a repeated seed inherited state from an earlier run")
		}

		// The source remains reusable; each artifact carries its own seed override.
		retained, err := os.ReadFile(inputPath)
		if err != nil || !bytes.Equal(original, retained) {
			t.Fatalf("series changed the source scenario: %v", err)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 4 {
			t.Fatalf("artifacts = %v (%v), want index and three reports", entries, err)
		}

		// A report's archived input can reproduce its model result via commit 4.
		replay := input
		replay.Execution.Seed = seeds[1]
		replayPath := writeScenarioCLIInput(t, replay)
		replayOutput := executeScenarioCLI(t, executable, "-scenario", replayPath)
		replayReport := decodeScenarioCLIReport(t, replayOutput, "finished")
		replayReport.Result.WallTime = ""
		if !reflect.DeepEqual(results[1], replayReport.Result) {
			t.Fatal("a series run differs from its standalone replay")
		}
	})

	t.Run("signed int64 boundaries remain exact", func(t *testing.T) {
		inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
		directory := filepath.Join(t.TempDir(), "boundaries")
		output := executeScenarioCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-seeds", "-9223372036854775808,9223372036854775807",
			"-output-dir", directory,
		)
		if output.err != nil {
			t.Fatalf("boundary seeds failed: %v\n%s", output.err, output.stderr)
		}
		index := decodeScenarioSeriesIndex(t, output.stdout, "finished")
		if len(index.Runs) != 2 || index.Runs[0].Seed != "-9223372036854775808" ||
			index.Runs[1].Seed != "9223372036854775807" {
			t.Fatalf("boundary seeds lost precision: %+v", index.Runs)
		}
		for _, run := range index.Runs {
			report := readScenarioSeriesRun(t, directory, run.Report, "finished")
			if strconv.FormatInt(report.Result.Seed, 10) != run.Seed {
				t.Fatalf("report and index seeds disagree: %+v", run)
			}
		}
	})

	t.Run("execution failures keep reports and continue the series", func(t *testing.T) {
		input := scenarioCLIDefinition()
		input.Execution.MaxEvents = 20
		inputPath := writeScenarioCLIInput(t, input)
		directory := filepath.Join(t.TempDir(), "failures")
		output := executeScenarioCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-seeds", "11,12",
			"-output-dir", directory,
		)
		var exit *exec.ExitError
		if !errors.As(output.err, &exit) || exit.ExitCode() == 0 {
			t.Fatalf("failed series returned %v, want nonzero exit", output.err)
		}
		index := decodeScenarioSeriesIndex(t, output.stdout, "failed")
		assertScenarioSeriesIndexSaved(t, directory, output.stdout)
		if len(index.Runs) != 2 {
			t.Fatalf("failed index lost the requested runs: %+v", index.Runs)
		}
		for _, run := range index.Runs {
			if run.Status != "failed" || run.Report == "" || run.Error == "" {
				t.Fatalf("failed run was not retained: %+v", run)
			}
			report := readScenarioSeriesRun(t, directory, run.Report, "failed")
			if report.Result.EventsExecuted != 20 || report.Result.PendingEvents == 0 {
				t.Fatalf("failed run did not preserve partial work: %+v", report.Result)
			}
		}
	})

	t.Run("malformed seeds create no directory", func(t *testing.T) {
		inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
		for _, seeds := range []string{
			"", " ", "1,", ",1", "1,,2", "1.5", "1e3", "1:3",
			"9223372036854775808", "-9223372036854775809",
		} {
			t.Run(seeds, func(t *testing.T) {
				directory := filepath.Join(t.TempDir(), "rejected")
				output := executeScenarioCLI(
					t,
					executable,
					"-scenario", inputPath,
					"-seeds", seeds,
					"-output-dir", directory,
				)
				assertScenarioCLIRejected(t, output)
				assertScenarioSeriesMissing(t, directory)
			})
		}
	})

	t.Run("incomplete or conflicting options are rejected", func(t *testing.T) {
		inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
		directory := filepath.Join(t.TempDir(), "rejected")
		cases := [][]string{
			{"-seeds", "1,2", "-output-dir", directory},
			{"-scenario", inputPath, "-seeds", "1,2"},
			{"-scenario", inputPath, "-output-dir", directory},
			{"-scenario", inputPath, "-seeds", "1", "-output-dir="},
			{
				"-scenario", inputPath, "-seeds", "1", "-output-dir", directory,
				"-output", filepath.Join(t.TempDir(), "run.json"),
			},
			{
				"-scenario", inputPath, "-seeds", "1", "-output-dir", directory,
				"-trace", filepath.Join(t.TempDir(), "run.trace"),
			},
		}
		for _, arguments := range cases {
			output := executeScenarioCLI(t, executable, arguments...)
			assertScenarioCLIRejected(t, output)
			assertScenarioSeriesMissing(t, directory)
		}
	})

	t.Run("input validation happens before directory creation", func(t *testing.T) {
		inputPath := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(inputPath, []byte(`{"unexpected":1}`), 0600); err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(t.TempDir(), "rejected")
		output := executeScenarioCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-seeds", "1,2",
			"-output-dir", directory,
		)
		assertScenarioCLIRejected(t, output)
		assertScenarioSeriesMissing(t, directory)
	})

	t.Run("existing output directory is preserved", func(t *testing.T) {
		inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
		directory := t.TempDir()
		retainedPath := filepath.Join(directory, "previous.json")
		previous := []byte("previous experiment")
		if err := os.WriteFile(retainedPath, previous, 0600); err != nil {
			t.Fatal(err)
		}
		output := executeScenarioCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-seeds", "1,2",
			"-output-dir", directory,
		)
		assertScenarioCLIRejected(t, output)
		retained, err := os.ReadFile(retainedPath)
		if err != nil || !bytes.Equal(previous, retained) {
			t.Fatalf("series overwrote an existing artifact: %v", err)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 1 {
			t.Fatalf("rejected series changed the directory: %v / %v", entries, err)
		}
	})

	t.Run("missing parent fails before any run", func(t *testing.T) {
		inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
		directory := filepath.Join(t.TempDir(), "missing-parent", "series")
		output := executeScenarioCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-seeds", "1,2",
			"-output-dir", directory,
		)
		assertScenarioCLIRejected(t, output)
		assertScenarioSeriesMissing(t, directory)
	})
}

// Cancellation before the first run leaves an honest index of unstarted work.
func TestScenarioSeriesCancellation(t *testing.T) {
	input := scenarioCLIDefinition()
	inputPath := writeScenarioCLIInput(t, input)
	directory := filepath.Join(t.TempDir(), "cancelled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var output bytes.Buffer
	err := runScenarioSeries(ctx, configuration{
		scenarioPath:    inputPath,
		seeds:           "1,2,3",
		outputDirectory: directory,
	}, &output)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled series returned %v", err)
	}
	index := decodeScenarioSeriesIndex(t, output.Bytes(), "cancelled")
	assertScenarioSeriesIndexSaved(t, directory, output.Bytes())
	if len(index.Runs) != 3 {
		t.Fatalf("cancellation lost requested runs: %+v", index.Runs)
	}
	for _, run := range index.Runs {
		if run.Status != "notStarted" || run.Report != "" {
			t.Fatalf("cancelled series started a future run: %+v", run)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "index.json" {
		t.Fatalf("cancelled series artifacts = %v / %v", entries, err)
	}
}

type scenarioSeriesIndex struct {
	Version  int             `json:"version"`
	Status   string          `json:"status"`
	Error    string          `json:"error"`
	Manifest json.RawMessage `json:"manifest"`
	Runs     []struct {
		ID     string `json:"id"`
		Seed   string `json:"seed"`
		Status string `json:"status"`
		Report string `json:"report"`
		Error  string `json:"error"`
	} `json:"runs"`
}

func decodeScenarioSeriesIndex(t *testing.T, encoded []byte, status string) scenarioSeriesIndex {
	t.Helper()
	var index scenarioSeriesIndex
	if err := json.Unmarshal(encoded, &index); err != nil {
		t.Fatalf("invalid series index: %v\n%s", err, encoded)
	}
	if index.Version != 1 || index.Status != status || len(index.Manifest) == 0 {
		t.Fatalf("unexpected series index: %+v", index)
	}
	return index
}

func assertScenarioSeriesIndexSaved(t *testing.T, directory string, stdout []byte) {
	t.Helper()
	stored, err := os.ReadFile(filepath.Join(directory, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk, terminal bytes.Buffer
	if err := json.Compact(&disk, stored); err != nil {
		t.Fatalf("invalid stored index: %v", err)
	}
	if err := json.Compact(&terminal, stdout); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(disk.Bytes(), terminal.Bytes()) {
		t.Fatal("stdout and persisted index disagree")
	}
}

func readScenarioSeriesRun(
	t *testing.T,
	directory, name, status string,
) scenarioCLIReport {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	var report scenarioCLIReport
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatalf("invalid run report: %v", err)
	}
	if report.Version != 1 || report.Status != status || len(report.Manifest) == 0 {
		t.Fatalf("unexpected run report: %+v", report)
	}
	return report
}

func assertScenarioSeriesMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid series created output directory: %v", err)
	}
}
