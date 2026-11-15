package scenario_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"load-balancer/scenario"
)

// Canonical language fixtures reach native admission without changing scalar runtime settings.
func TestSharedFamilyScenarioFixtures(t *testing.T) {
	directory := filepath.Join("..", "..", "..", "..", "..", "examples", "randomness", "families")
	files, err := filepath.Glob(filepath.Join(directory, "*-scenario.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("compiled distribution families need native scenario fixtures")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			input, err := scenario.DecodeDocument(data)
			if err != nil {
				t.Fatal(err)
			}

			plan, err := scenario.Compile(input)
			if err != nil {
				t.Fatal(err)
			}

			state, err := plan.StateAt(0)
			if err != nil {
				t.Fatal(err)
			}

			// Unbound definitions preserve the baseline CPU cost and link travel time.
			for _, backend := range state.Backends {
				if backend.Host.PacketProcessingTime != 0 {
					t.Fatalf("%s: CPU cost changed to %v", backend.ID, backend.Host.PacketProcessingTime)
				}
				if backend.Network.ToLoadBalancer.PacketTravelTime != time.Millisecond {
					t.Fatalf("%s: link travel time changed to %v", backend.ID, backend.Network.ToLoadBalancer.PacketTravelTime)
				}
			}

			encoded, err := scenario.EncodeDocument(input)
			if err != nil {
				t.Fatal(err)
			}

			restored, err := scenario.DecodeDocument(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored.Fixed.RandomProfiles, input.Fixed.RandomProfiles) {
				t.Fatal("native roundtrip changed exact distribution definitions")
			}
		})
	}
}
