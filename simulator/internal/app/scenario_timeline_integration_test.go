package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"load-balancer/internal/contract"
	"load-balancer/internal/testsupport"
)

func assertTimelineResult(t *testing.T, result contract.SimulationResult) {
	t.Helper()
	if result.State.Algorithm != "RoundRobin" || result.Ingress.Config.BandwidthBitsPerSecond != 16_000_000 {
		t.Fatalf("later LB configuration was not executed: algorithm=%s ingress=%d",
			result.State.Algorithm, result.Ingress.Config.BandwidthBitsPerSecond)
	}
	if result.CreatedClients != 4 || result.CompletedStreams != 4 ||
		result.ReceivedPayloadBytes != 4150 || result.AcknowledgedPayloadBytes != 4150 {
		t.Fatalf("later generator was not executed: clients=%d streams=%d payload=%d/%d",
			result.CreatedClients, result.CompletedStreams,
			result.ReceivedPayloadBytes, result.AcknowledgedPayloadBytes)
	}
	foundName := false
	for _, backend := range result.State.Servers {
		foundName = foundName || backend.Server.Name == "after-transition"
	}
	if !foundName {
		t.Fatal("later backend settings were lost")
	}
}

func TestScenarioCLIExecutesAndArchivesCompleteTimeline(t *testing.T) {
	executable := testsupport.BuildCLI(t)
	input := testsupport.TimelineScenario()
	path := testsupport.WriteCLIInput(t, input)
	report := testsupport.DecodeCLIReport(t, testsupport.ExecuteCLI(t, executable, "-scenario", path), "finished")
	assertTimelineResult(t, report.Result)
	testsupport.AssertCLIManifest(t, report.Manifest, input, false)

	// Each seed recompiles the same complete timeline into an independent world.
	directory := filepath.Join(t.TempDir(), "timeline-series")
	output := testsupport.ExecuteCLI(t, executable, "-scenario", path,
		"-seeds", "-7,11,11", "-output-dir", directory)
	if output.Err != nil {
		t.Fatalf("timeline series: %v\n%s", output.Err, output.Stderr)
	}
	index := decodeScenarioSeriesIndex(t, output.Stdout, "finished")
	testsupport.AssertCLIManifest(t, index.Manifest, input, false)
	var previous contract.SimulationResult
	for position, seed := range []int64{-7, 11, 11} {
		entry := index.Runs[position]
		if entry.Seed != strconv.FormatInt(seed, 10) {
			t.Fatalf("series seed = %s, want %d", entry.Seed, seed)
		}
		run := readScenarioSeriesRun(t, directory, entry.Report, "finished")
		requested := input
		requested.Fixed.Execution.Seed = seed
		testsupport.AssertCLIManifest(t, run.Manifest, requested, false)
		assertTimelineResult(t, run.Result)
		if run.Result.Seed != seed {
			t.Fatalf("runtime seed = %d, archived seed = %d", run.Result.Seed, seed)
		}
		run.Result.WallTime = ""
		if position == 2 && !reflect.DeepEqual(previous, run.Result) {
			t.Fatal("repeated seed inherited state from the previous timeline run")
		}
		previous = run.Result
	}

	// Invalid late settings fail before output or trace files are reserved.
	invalid := testsupport.TimelineScenario()
	later := invalid.States["40000000"]
	later.Resources.LoadBalancer.Algorithm = "invalid"
	invalid.States["40000000"] = later
	invalidPath := testsupport.WriteCLIInput(t, invalid)
	reportPath := filepath.Join(t.TempDir(), "invalid-report.json")
	tracePath := filepath.Join(t.TempDir(), "invalid.trace")
	rejected := testsupport.ExecuteCLI(t, executable, "-scenario", invalidPath,
		"-output", reportPath, "-trace", tracePath)
	testsupport.AssertCLIRejected(t, rejected)
	for _, path := range []string{reportPath, tracePath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid later state created %s: %v", path, err)
		}
	}
}
