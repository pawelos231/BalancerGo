package scenario_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"load-balancer/scenario"
)

// Contract examples preserve their timestamps, profiles and explicit overrides
// when decoded and written again, before execution.
func TestScenarioExamplesRoundTrip(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "examples", "scenarios", "timeline-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no timeline contract examples found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			document, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			input, err := scenario.DecodeDocument(document)
			if err != nil {
				t.Fatal(err)
			}

			// JSON omits optional empty lists: [] and an absent list mean the same thing.
			if len(input.Fixed.Network.Profiles) == 0 {
				input.Fixed.Network.Profiles = nil
			}
			for _, state := range input.States {
				for i := range state.Workload.ClientGroups {
					group := &state.Workload.ClientGroups[i]
					if len(group.Overrides) == 0 {
						group.Overrides = nil
					}
				}
			}

			encoded, err := scenario.EncodeDocument(input)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := scenario.DecodeDocument(encoded)
			if err != nil || !reflect.DeepEqual(restored, input) {
				t.Fatalf("example changed during round trip: %v", err)
			}

			// The complete timeline validates before runtime admission.
			resolved, err := scenario.Normalize(input)
			if err != nil {
				t.Fatalf("example failed timeline validation: %v", err)
			}
			if len(resolved.States) != len(input.States) || resolved.States[0].At != 0 {
				t.Fatal("normalized example lost its initial state or transitions")
			}

			// Compilation prepares every transition before world construction.
			plan, err := scenario.Compile(input)
			if err != nil {
				t.Fatalf("example failed timeline compilation: %v", err)
			}
			if _, err := plan.StateAt(input.Fixed.Execution.Duration); err != nil {
				t.Fatalf("compiled example cannot describe its terminal configuration: %v", err)
			}
		})
	}
}
