package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

// CLI tests execute the built program, including flags, files and exit codes.
// The browser's scenario encoder produces the exact same input documents.
func TestScenarioCLI(t *testing.T) {
	executable := buildScenarioCLI(t)

	t.Run("full scenario is repeatable", func(t *testing.T) {
		input := scenarioCLIDefinition()
		path := writeScenarioCLIInput(t, input)

		firstOutput := executeScenarioCLI(t, executable, "-scenario", path)
		first := decodeScenarioCLIReport(t, firstOutput, "finished")
		secondOutput := executeScenarioCLI(t, executable, "-scenario", path)
		second := decodeScenarioCLIReport(t, secondOutput, "finished")
		assertScenarioCLIManifest(t, first.Manifest, input, false)

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
		if first.Result.Seed != input.Execution.Seed {
			t.Fatalf("seed = %d, want %d", first.Result.Seed, input.Execution.Seed)
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
		input := scenarioCLIDefinition()
		capacity := 64
		input.TCP.ReceiveBufferBytes = &capacity
		path := writeScenarioCLIInput(t, input)

		output := executeScenarioCLI(t, executable, "-scenario", path)
		report := decodeScenarioCLIReport(t, output, "finished")
		assertScenarioCLIManifest(t, report.Manifest, input, false)
		if report.Result.TCP.ReceiveBufferBytes != capacity || report.Result.CompletedStreams != 3 {
			t.Fatalf("receive buffer was not applied to the run: %+v", report.Result)
		}
		if report.Result.ReceivedPayloadBytes != 4131 || report.Result.AcknowledgedPayloadBytes != 4131 {
			t.Fatalf("small receive buffer changed payload totals: %+v", report.Result)
		}
	})

	t.Run("result can be written to a file", func(t *testing.T) {
		inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
		outputPath := filepath.Join(t.TempDir(), "run.json")
		output := executeScenarioCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-output", outputPath,
			"-headless",
		)
		if output.err != nil {
			t.Fatalf("run failed: %v\n%s", output.err, output.stderr)
		}
		if len(bytes.TrimSpace(output.stdout)) != 0 {
			t.Fatalf("file output also printed a report to stdout: %s", output.stdout)
		}
		encoded, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		output.stdout = encoded
		decodeScenarioCLIReport(t, output, "finished")
	})

	t.Run("runtime trace is recorded in the manifest", func(t *testing.T) {
		input := scenarioCLIDefinition()
		inputPath := writeScenarioCLIInput(t, input)
		tracePath := filepath.Join(t.TempDir(), "run.trace")
		output := executeScenarioCLI(
			t,
			executable,
			"-scenario", inputPath,
			"-trace", tracePath,
		)
		report := decodeScenarioCLIReport(t, output, "finished")
		assertScenarioCLIManifest(t, report.Manifest, input, true)
		trace, err := os.Stat(tracePath)
		if err != nil || trace.Size() == 0 {
			t.Fatalf("runtime trace was not written: %v", err)
		}
	})

	t.Run("event limit retains a failed report", func(t *testing.T) {
		input := scenarioCLIDefinition()
		input.Execution.MaxEvents = 20
		path := writeScenarioCLIInput(t, input)
		output := executeScenarioCLI(t, executable, "-scenario", path)
		report := decodeScenarioCLIReport(t, output, "failed")

		if report.Result.EventsExecuted != 20 || report.Result.PendingEvents == 0 {
			t.Fatalf("partial run counters = %+v", report.Result)
		}
		if !strings.Contains(report.Error, "event execution limit") {
			t.Fatalf("failure reason = %q", report.Error)
		}
	})

	t.Run("invalid documents leave no report", func(t *testing.T) {
		valid, err := scenario.EncodeDocument(scenarioCLIDefinition())
		if err != nil {
			t.Fatal(err)
		}
		invalid := scenarioCLIDefinition()
		invalid.Resources.LoadBalancer.ForwardingBitsPerSecond = 0
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
				name:     "unknown field",
				document: append([]byte(`{"unexpected":1,`), valid[1:]...),
				message:  "unknown field",
			},
			{
				name:     "duplicate field",
				document: append([]byte(`{"version":1,`), valid[1:]...),
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
				output := executeScenarioCLI(
					t,
					executable,
					"-scenario", inputPath,
					"-output", outputPath,
				)
				assertScenarioCLIRejected(t, output)
				if !strings.Contains(output.stderr, test.message) {
					t.Fatalf("validation error = %q, want %q", output.stderr, test.message)
				}
				if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("invalid input created a report: %v", err)
				}
			})
		}
	})

	t.Run("invalid scenario flags are rejected", func(t *testing.T) {
		inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
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
			output := executeScenarioCLI(t, executable, arguments...)
			assertScenarioCLIRejected(t, output)
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
				inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
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
				output := executeScenarioCLI(
					t,
					executable,
					"-scenario", inputPath,
					test.flag, existingPath,
				)
				assertScenarioCLIRejected(t, output)
				retained, err := os.ReadFile(existingPath)
				if err != nil || !bytes.Equal(original, retained) {
					t.Fatalf("%s overwrote an existing file: %v", test.flag, err)
				}
			})
		}
	})

	t.Run("file setup errors leave no report", func(t *testing.T) {
		inputPath := writeScenarioCLIInput(t, scenarioCLIDefinition())
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
				output := executeScenarioCLI(t, executable, arguments...)
				assertScenarioCLIRejected(t, output)
				if _, err := os.Stat(test.outputPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("setup failure retained a report: %v", err)
				}
			})
		}
	})

	t.Run("legacy flags keep the existing report", func(t *testing.T) {
		output := executeScenarioCLI(
			t,
			executable,
			"-headless",
			"-clients", "2",
			"-servers", "1",
			"-bytes", "2050",
			"-duration", "100ms",
			"-packet-travel-time", "0ms",
		)
		if output.err != nil {
			t.Fatalf("legacy run failed: %v\n%s", output.err, output.stderr)
		}
		var result contract.SimulationResult
		if err := json.Unmarshal(output.stdout, &result); err != nil {
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
	input := scenarioCLIDefinition()
	path := writeScenarioCLIInput(t, input)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var output bytes.Buffer
	err := runScenarioFile(ctx, configuration{scenarioPath: path}, &output)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled runner returned %v", err)
	}
	var report scenarioCLIReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("cancelled run did not produce a report: %v", err)
	}
	if report.Status != "cancelled" || report.Error != context.Canceled.Error() {
		t.Fatalf("cancelled outcome = %q / %q", report.Status, report.Error)
	}
	if report.Result.EventsExecuted != 0 || report.Result.PendingEvents == 0 {
		t.Fatalf("cancelled run misreported completed work: %+v", report.Result)
	}
	assertScenarioCLIManifest(t, report.Manifest, input, false)
}

// A large seed exercises exact integer handling; slow playback must be ignored.
func scenarioCLIDefinition() scenario.Scenario {
	input := scenarioRuntimeDefinition()
	input.Execution.Seed = 9_007_199_254_740_993
	input.Execution.Speed = 0.001
	input.Resources.LoadBalancer.Algorithm = "random"
	input.Workload.Defaults.Bytes = 2050
	input.Network.Profiles = []scenario.LinkProfile{
		{
			ID: "fast",
			Config: scenario.LinkPatch{
				BandwidthBitsPerSecond: scenarioRuntimeValue(int64(80_000_000)),
			},
		},
		{
			ID: "slow",
			Config: scenario.LinkPatch{
				BandwidthBitsPerSecond: scenarioRuntimeValue(int64(4_000_000)),
				PacketTravelTime:       scenarioRuntimeValue(time.Millisecond),
			},
		},
	}
	input.Resources.Backends[0].Network.Profile = "fast"
	input.Resources.Backends[1].Network.Profile = "slow"
	input.Workload.ClientGroups = []scenario.ClientGroup{
		{
			ID:      "fast",
			Count:   2,
			Network: scenario.EndpointNetworkConfig{Profile: "fast"},
			Overrides: []scenario.ClientOverride{
				{
					Index:  1,
					Stream: scenario.StreamPatch{Bytes: scenarioRuntimeValue(31)},
				},
			},
		},
		{
			ID:      "slow",
			Count:   1,
			StartAt: 20 * time.Millisecond,
			Network: scenario.EndpointNetworkConfig{Profile: "slow"},
		},
	}
	return input
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

func assertScenarioCLIManifest(
	t *testing.T,
	encoded json.RawMessage,
	input scenario.Scenario,
	traceEnabled bool,
) {
	t.Helper()
	var manifest struct {
		Model          string          `json:"model"`
		Scenario       json.RawMessage `json:"scenario"`
		ScenarioSHA256 string          `json:"scenarioSha256"`
		Execution      struct {
			Headless     bool    `json:"headless"`
			Speed        float64 `json:"speed"`
			TraceEnabled bool    `json:"traceEnabled"`
		} `json:"execution"`
		Build struct {
			GoVersion     string `json:"goVersion"`
			ModuleVersion string `json:"moduleVersion"`
			GOOS          string `json:"goos"`
			GOARCH        string `json:"goarch"`
		} `json:"build"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Model != "tcp-receive-window-stop-and-wait-v1" {
		t.Fatalf("model profile = %q", manifest.Model)
	}
	if !manifest.Execution.Headless || manifest.Execution.Speed != 0 ||
		manifest.Execution.TraceEnabled != traceEnabled {
		t.Fatalf("actual execution settings = %+v", manifest.Execution)
	}
	if manifest.Build.GoVersion == "" || manifest.Build.ModuleVersion == "" ||
		manifest.Build.GOOS != runtime.GOOS || manifest.Build.GOARCH != runtime.GOARCH {
		t.Fatalf("missing or incorrect build identity: %+v", manifest.Build)
	}

	// The archived input retains requested playback and exact large integers.
	canonical, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, manifest.Scenario); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compact.Bytes(), canonical) {
		t.Fatal("manifest scenario differs from the input document")
	}
	checksum := sha256.Sum256(canonical)
	if manifest.ScenarioSHA256 != hex.EncodeToString(checksum[:]) {
		t.Fatalf("manifest checksum = %q", manifest.ScenarioSHA256)
	}
	restored, err := scenario.DecodeDocument(manifest.Scenario)
	if err != nil {
		t.Fatalf("stored scenario cannot be replayed: %v", err)
	}
	if restored.Execution.Seed != input.Execution.Seed ||
		restored.Execution.Speed != input.Execution.Speed {
		t.Fatal("stored scenario lost the original seed or playback settings")
	}
}

type scenarioCLIOutput struct {
	stdout []byte
	stderr string
	err    error
}

type scenarioCLIReport struct {
	Version  int                       `json:"version"`
	Status   string                    `json:"status"`
	Error    string                    `json:"error"`
	Manifest json.RawMessage           `json:"manifest"`
	Result   contract.SimulationResult `json:"result"`
}

func assertScenarioCLIRejected(t *testing.T, output scenarioCLIOutput) {
	t.Helper()
	var exit *exec.ExitError
	if !errors.As(output.err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("invalid request returned %v, want a nonzero exit", output.err)
	}
	if len(bytes.TrimSpace(output.stdout)) != 0 {
		t.Fatalf("rejected request printed a report: %s", output.stdout)
	}
	if strings.TrimSpace(output.stderr) == "" {
		t.Fatal("rejected request returned no explanation")
	}
}

func buildScenarioCLI(t *testing.T) string {
	t.Helper()
	name := "simulator"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "build", "-o", path, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return path
}

func executeScenarioCLI(
	t *testing.T,
	executable string,
	arguments ...string,
) scenarioCLIOutput {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, arguments...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI did not finish an unpaced run: %v\n%s", ctx.Err(), stderr.String())
	}
	return scenarioCLIOutput{
		stdout: stdout.Bytes(),
		stderr: stderr.String(),
		err:    err,
	}
}

func writeScenarioCLIInput(t *testing.T, input scenario.Scenario) string {
	t.Helper()
	encoded, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeScenarioCLIReport(
	t *testing.T,
	output scenarioCLIOutput,
	status string,
) scenarioCLIReport {
	t.Helper()
	if status == "finished" && output.err != nil {
		t.Fatalf("run failed: %v\n%s", output.err, output.stderr)
	}
	if status == "failed" {
		var exit *exec.ExitError
		if !errors.As(output.err, &exit) || exit.ExitCode() == 0 {
			t.Fatalf("failed run returned %v, want a nonzero process exit", output.err)
		}
	}
	var report scenarioCLIReport
	if err := json.Unmarshal(output.stdout, &report); err != nil {
		t.Fatalf("invalid CLI report: %v\n%s\n%s", err, output.stdout, output.stderr)
	}
	if report.Version != 1 || report.Status != status || len(report.Manifest) == 0 {
		t.Fatalf("report header = %+v", report)
	}
	return report
}
