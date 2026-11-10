package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"load-balancer/internal/contract"
	"load-balancer/internal/testsupport"
	"load-balancer/scenario"
)

// CLI tests execute the built program, including flags, files and exit codes.
// The browser's scenario encoder produces the exact same input documents.
func TestScenarioCLI(t *testing.T) {
	executable := testsupport.BuildCLI(t)

	t.Run("full scenario is repeatable", func(t *testing.T) {
		input := testsupport.CLIScenario()
		path := testsupport.WriteCLIInput(t, input)

		firstOutput := testsupport.ExecuteCLI(t, executable, "-scenario", path)
		first := testsupport.DecodeCLIReport(t, firstOutput, "finished")
		secondOutput := testsupport.ExecuteCLI(t, executable, "-scenario", path)
		second := testsupport.DecodeCLIReport(t, secondOutput, "finished")
		testsupport.AssertCLIManifest(t, first.Manifest, input, false)

		// A partial final segment and per-client overrides must survive the CLI.
		if first.Result.CreatedClients != 3 || first.Result.CompletedStreams != 3 {
			t.Fatalf("unexpected transfer counts: %+v", first.Result)
		}
		if first.Result.ReceivedPayloadBytes != 4131 ||
			first.Result.AcknowledgedPayloadBytes != 4131 {
			t.Fatalf("payload totals = %d / %d, want 4131 / 4131",
				first.Result.ReceivedPayloadBytes,
				first.Result.AcknowledgedPayloadBytes,
			)
		}
		if first.Result.Seed != input.Fixed.Execution.Seed {
			t.Fatalf("seed = %d, want %d", first.Result.Seed, input.Fixed.Execution.Seed)
		}
		if first.Result.TCP.ReceiveBufferBytes != 65535 {
			t.Fatalf("default receive buffer missing from report: %+v", first.Result.TCP)
		}
		assertScenarioCLILinks(t, first.Result)

		// Model results repeat; only measured execution time may differ.
		first.Result.WallTime = ""
		second.Result.WallTime = ""
		if !reflect.DeepEqual(first.Result, second.Result) {
			t.Fatal("identical scenario and seed produced different model results")
		}
	})

	t.Run("small receive buffer survives the scenario and report", func(t *testing.T) {
		input := testsupport.CLIScenario()
		capacity := 64
		input.Fixed.TCP.ReceiveBufferBytes = &capacity
		path := testsupport.WriteCLIInput(t, input)

		output := testsupport.ExecuteCLI(t, executable, "-scenario", path)
		report := testsupport.DecodeCLIReport(t, output, "finished")
		testsupport.AssertCLIManifest(t, report.Manifest, input, false)
		if report.Result.TCP.ReceiveBufferBytes != capacity || report.Result.CompletedStreams != 3 {
			t.Fatalf("receive buffer was not applied to the run: %+v", report.Result)
		}
		if report.Result.ReceivedPayloadBytes != 4131 || report.Result.AcknowledgedPayloadBytes != 4131 {
			t.Fatalf("small receive buffer changed payload totals: %+v", report.Result)
		}
	})

	t.Run("result can be written to a file", func(t *testing.T) {
		inputPath := testsupport.WriteCLIInput(t, testsupport.CLIScenario())
		outputPath := filepath.Join(t.TempDir(), "run.json")
		output := testsupport.ExecuteCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-output", outputPath,
			"-headless",
		)
		if output.Err != nil {
			t.Fatalf("run failed: %v\n%s", output.Err, output.Stderr)
		}
		if len(bytes.TrimSpace(output.Stdout)) != 0 {
			t.Fatalf("file output also printed a report to stdout: %s", output.Stdout)
		}
		encoded, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		output.Stdout = encoded
		testsupport.DecodeCLIReport(t, output, "finished")
	})

	t.Run("runtime trace is recorded in the manifest", func(t *testing.T) {
		input := testsupport.CLIScenario()
		inputPath := testsupport.WriteCLIInput(t, input)
		tracePath := filepath.Join(t.TempDir(), "run.trace")
		output := testsupport.ExecuteCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-trace", tracePath,
		)
		report := testsupport.DecodeCLIReport(t, output, "finished")
		testsupport.AssertCLIManifest(t, report.Manifest, input, true)
		trace, err := os.Stat(tracePath)
		if err != nil || trace.Size() == 0 {
			t.Fatalf("runtime trace was not written: %v", err)
		}
	})

	t.Run("event limit retains a failed report", func(t *testing.T) {
		input := testsupport.CLIScenario()
		input.Fixed.Execution.MaxEvents = 20
		path := testsupport.WriteCLIInput(t, input)
		output := testsupport.ExecuteCLI(t, executable, "-scenario", path)
		report := testsupport.DecodeCLIReport(t, output, "failed")

		if report.Result.EventsExecuted != 20 || report.Result.PendingEvents == 0 {
			t.Fatalf("partial run counters = %+v", report.Result)
		}
		if !strings.Contains(report.Error, "event execution limit") {
			t.Fatalf("failure reason = %q", report.Error)
		}
	})

	t.Run("invalid documents leave no report", func(t *testing.T) {
		valid, err := scenario.EncodeDocument(testsupport.CLIScenario())
		if err != nil {
			t.Fatal(err)
		}
		invalid := testsupport.CLIScenario()
		invalidState := invalid.States["0"]
		invalidState.Resources.LoadBalancer.ForwardingBitsPerSecond = 0
		invalid.States["0"] = invalidState
		invalidSettings, err := scenario.EncodeDocument(invalid)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name     string
			document []byte
			message  string
		}{
			{
				name:     "legacy version",
				document: []byte(`{"version":1}`),
				message:  "scenario.version: unknown field",
			},
			{
				name:     "unknown field",
				document: append([]byte(`{"unexpected":1,`), valid[1:]...),
				message:  "unknown field",
			},
			{
				name:     "duplicate field",
				document: append([]byte(`{"scenarioVersion":2,`), valid[1:]...),
				message:  "duplicate field",
			},
			{
				name:     "trailing document",
				document: append(append([]byte(nil), valid...), []byte(`{}`)...),
				message:  "trailing JSON",
			},
			{
				name:     "invalid setting",
				document: invalidSettings,
				message:  "forwardingBitsPerSecond",
			},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				directory := t.TempDir()
				inputPath := filepath.Join(directory, "invalid.json")
				outputPath := filepath.Join(directory, "run.json")
				if err := os.WriteFile(inputPath, test.document, 0600); err != nil {
					t.Fatal(err)
				}
				output := testsupport.ExecuteCLI(
					t,
					executable,
					"-scenario", inputPath,
					"-output", outputPath,
				)
				testsupport.AssertCLIRejected(t, output)
				if !strings.Contains(output.Stderr, test.message) {
					t.Fatalf("validation error = %q, want %q", output.Stderr, test.message)
				}
				if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("invalid input created a report: %v", err)
				}
			})
		}
	})

	t.Run("invalid scenario flags are rejected", func(t *testing.T) {
		inputPath := testsupport.WriteCLIInput(t, testsupport.CLIScenario())
		emptyScenarioReport := filepath.Join(t.TempDir(), "empty-scenario-run.json")
		cases := [][]string{
			{"-scenario", inputPath, "-seed", "1"},
			{"-scenario", inputPath, "-speed", "0"},
			{"-output", filepath.Join(t.TempDir(), "run.json")},
			{"-scenario="},
			{"-scenario=", "-output", emptyScenarioReport},
			{"-scenario", inputPath, "-output="},
		}
		for _, arguments := range cases {
			output := testsupport.ExecuteCLI(t, executable, arguments...)
			testsupport.AssertCLIRejected(t, output)
		}

		// Empty input cannot enter server mode or leave an experiment artifact.
		if _, err := os.Stat(emptyScenarioReport); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("empty scenario flag created a report: %v", err)
		}
	})

	t.Run("existing input report and trace are preserved", func(t *testing.T) {
		cases := []struct {
			name       string
			flag       string
			inputAlias bool
		}{
			{name: "report aliases input", flag: "-output", inputAlias: true},
			{name: "trace aliases input", flag: "-trace", inputAlias: true},
			{name: "existing report", flag: "-output"},
			{name: "existing trace", flag: "-trace"},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				inputPath := testsupport.WriteCLIInput(t, testsupport.CLIScenario())
				existingPath := inputPath
				if !test.inputAlias {
					existingPath = filepath.Join(t.TempDir(), "previous.artifact")
					if err := os.WriteFile(existingPath, []byte("previous run"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				original, err := os.ReadFile(existingPath)
				if err != nil {
					t.Fatal(err)
				}

				// Neither source input nor artifacts from another run may be replaced.
				output := testsupport.ExecuteCLI(
					t,
					executable,
					"-scenario", inputPath,
					test.flag, existingPath,
				)
				testsupport.AssertCLIRejected(t, output)
				retained, err := os.ReadFile(existingPath)
				if err != nil || !bytes.Equal(original, retained) {
					t.Fatalf("%s overwrote an existing file: %v", test.flag, err)
				}
			})
		}
	})

	t.Run("file setup errors leave no report", func(t *testing.T) {
		inputPath := testsupport.WriteCLIInput(t, testsupport.CLIScenario())
		directory := t.TempDir()
		cases := []struct {
			name       string
			inputPath  string
			outputPath string
			tracePath  string
		}{
			{
				name:       "missing input",
				inputPath:  filepath.Join(directory, "missing.json"),
				outputPath: filepath.Join(directory, "missing-input-report.json"),
			},
			{
				name:       "missing output directory",
				inputPath:  inputPath,
				outputPath: filepath.Join(directory, "missing-directory", "run.json"),
			},
			{
				name:       "output and trace share a new path",
				inputPath:  inputPath,
				outputPath: filepath.Join(directory, "shared-artifact"),
				tracePath:  filepath.Join(directory, "shared-artifact"),
			},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				arguments := []string{
					"-scenario", test.inputPath,
					"-output", test.outputPath,
				}
				if test.tracePath != "" {
					arguments = append(arguments, "-trace", test.tracePath)
				}
				output := testsupport.ExecuteCLI(t, executable, arguments...)
				testsupport.AssertCLIRejected(t, output)
				if _, err := os.Stat(test.outputPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("setup failure retained a report: %v", err)
				}
			})
		}
	})

	t.Run("legacy flags keep the existing report", func(t *testing.T) {
		output := testsupport.ExecuteCLI(
			t,
			executable,
			"-headless",
			"-clients", "2",
			"-servers", "1",
			"-bytes", "2050",
			"-duration", "100ms",
			"-packet-travel-time", "0ms",
		)
		if output.Err != nil {
			t.Fatalf("legacy run failed: %v\n%s", output.Err, output.Stderr)
		}
		var result contract.SimulationResult
		if err := json.Unmarshal(output.Stdout, &result); err != nil {
			t.Fatal(err)
		}
		if result.CreatedClients != 2 || result.CompletedStreams != 2 ||
			result.AcknowledgedPayloadBytes != 4100 {
			t.Fatalf("legacy result changed: %+v", result)
		}
	})
}

// Cancellation must still save an honest partial run for experiment tooling.
// A cancelled context exercises the same path without platform-specific signals.
func TestScenarioCLICancellation(t *testing.T) {
	input := testsupport.CLIScenario()
	path := testsupport.WriteCLIInput(t, input)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var output bytes.Buffer
	err := runScenarioFile(ctx, configuration{scenarioPath: path}, &output)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled runner returned %v", err)
	}
	var report testsupport.CLIReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("cancelled run did not produce a report: %v", err)
	}
	if report.Status != "cancelled" || report.Error != context.Canceled.Error() {
		t.Fatalf("cancelled outcome = %q / %q", report.Status, report.Error)
	}
	if report.Result.EventsExecuted != 0 || report.Result.PendingEvents == 0 {
		t.Fatalf("cancelled run misreported completed work: %+v", report.Result)
	}
	testsupport.AssertInProcessManifest(t, report.Manifest, input, false)
}

func assertScenarioCLILinks(t *testing.T, result contract.SimulationResult) {
	t.Helper()
	rates := make(map[string]int64, len(result.Links))
	for _, link := range result.Links {
		rates[link.Name] = link.Config.BandwidthBitsPerSecond
	}
	if rates["client/fast/0 -> LB input"] != 80_000_000 ||
		rates["client/slow/0 -> LB input"] != 4_000_000 {
		t.Fatalf("link profiles were not applied: %v", rates)
	}
}
