package scenario_test

import (
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
)

func TestLBClosingAndResetIdleTimeouts(t *testing.T) {
	input := exampleScenario()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.States[0].LoadBalancer.ClosingIdleTimeout != 120*time.Second ||
		resolved.States[0].LoadBalancer.ResetIdleTimeout != 10*time.Second {
		t.Fatalf("LB idle defaults = %+v", resolved.States[0].LoadBalancer)
	}
	if input.States["0"].Resources.LoadBalancer.MappingTimeouts.ClosingIdleTimeout != nil ||
		input.States["0"].Resources.LoadBalancer.MappingTimeouts.ResetIdleTimeout != nil {
		t.Fatal("normalization changed omitted idle limits")
	}

	// Native documents retain integers above the exact float64 range and explicit zeroes.
	for _, limits := range [][2]time.Duration{{0, 9007199254740993}, {9007199254740995, 0}, {1, 2}} {
		editInitialState(&input, func(state *scenario.StateConfig) {
			state.Resources.LoadBalancer.MappingTimeouts.ClosingIdleTimeout = &limits[0]
		})
		editInitialState(&input, func(state *scenario.StateConfig) {
			state.Resources.LoadBalancer.MappingTimeouts.ResetIdleTimeout = &limits[1]
		})
		document, err := scenario.EncodeDocument(input)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := scenario.DecodeDocument(document)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err = scenario.Normalize(restored)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.States[0].LoadBalancer.ClosingIdleTimeout != limits[0] ||
			resolved.States[0].LoadBalancer.ResetIdleTimeout != limits[1] {
			t.Fatalf("LB idle limits changed: %+v", resolved.States[0].LoadBalancer)
		}
	}
}

func TestLBClosingAndResetIdleTimeoutsRejectNegativeDurations(t *testing.T) {
	for _, phase := range []string{"closing", "reset"} {
		t.Run(phase, func(t *testing.T) {
			input := exampleScenario()
			negative := -time.Nanosecond
			if phase == "closing" {
				editInitialState(&input, func(state *scenario.StateConfig) {
					state.Resources.LoadBalancer.MappingTimeouts.ClosingIdleTimeout = &negative
				})
			} else {
				editInitialState(&input, func(state *scenario.StateConfig) {
					state.Resources.LoadBalancer.MappingTimeouts.ResetIdleTimeout = &negative
				})
			}
			_, err := scenario.Normalize(input)
			path := "resources.loadBalancer.mappingTimeouts." + phase + "IdleTimeoutNanoseconds"
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected %s error, got %v", path, err)
			}
		})
	}
}
