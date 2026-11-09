package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

// A new controller represents a backend restart. Its first run must not reuse
// the previous controller's first ID, even for exactly the same scenario.
func TestRunIdentitySurvivesControllerRestart(t *testing.T) {
	input := testConfiguration().scenarioDefinition()
	resolved, err := scenario.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}

	seen := make(map[string]bool)
	for range 2 {
		control := newRunController(context.Background())

		// Also start a second run on each controller, after releasing its first run.
		for range 2 {
			receipt, err := control.startTimeline(context.Background(), document, resolved)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.RunID == "" || seen[receipt.RunID] {
				t.Fatalf("run identity was empty or reused: %q", receipt.RunID)
			}
			seen[receipt.RunID] = true

			request := <-control.starts
			record := control.currentRun()
			if request.runID != receipt.RunID || record.RunID != receipt.RunID {
				t.Fatal("accepted request and stored record have different identities")
			}
			if !reflect.DeepEqual(request.experiment, record.Experiment) {
				t.Fatal("the simulation owner received different experiment metadata")
			}
			assertInteractiveExperiment(t, document, resolved.Fixed().Execution.Speed, record.Experiment)
			control.finishRun()
		}
	}
}

// Reading a run must not expose the controller's stored document or optional
// build flag: editing the returned copy must leave the next read unchanged.
func TestCurrentRunMetadataIsDetached(t *testing.T) {
	control := newRunController(context.Background())
	modified := true
	control.rememberRun("test-run", []byte(`{"scenarioVersion":2}`), contract.ExperimentContext{
		Build: contract.ExperimentBuild{VCSModified: &modified},
	})

	copy := control.currentRun()
	copy.Scenario[0] = '!'
	copy.Experiment.Model = "changed"
	*copy.Experiment.Build.VCSModified = false

	original := control.currentRun()
	if original.Scenario[0] != '{' || original.Experiment.Model != "" ||
		!(*original.Experiment.Build.VCSModified) {
		t.Fatal("editing a returned record changed the controller's metadata")
	}
}

// The hash identifies the canonical request, including exact integer strings.
// CLI and interactive execution share provenance but record different pacing.
func assertInteractiveExperiment(
	t *testing.T,
	document []byte,
	speed float64,
	experiment contract.ExperimentContext,
) {
	t.Helper()
	checksum := sha256.Sum256(document)
	if experiment.ScenarioSHA256 != hex.EncodeToString(checksum[:]) {
		t.Fatal("experiment hash does not identify the stored scenario")
	}
	if experiment.Execution.Headless || experiment.Execution.TraceEnabled ||
		experiment.Execution.Speed != speed {
		t.Fatalf("incorrect interactive execution settings: %+v", experiment.Execution)
	}

	cli := experimentManifest(document, false)
	if experiment.Model != cli.Model || !reflect.DeepEqual(experiment.Build, cli.Build) {
		t.Fatal("CLI and interactive runs use different model or build metadata")
	}
}
