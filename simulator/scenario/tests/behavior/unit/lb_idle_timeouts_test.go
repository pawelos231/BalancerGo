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
	if resolved.LoadBalancer.ClosingIdleTimeout != 120*time.Second ||
		resolved.LoadBalancer.ResetIdleTimeout != 10*time.Second {
		t.Fatalf("LB idle defaults = %+v", resolved.LoadBalancer)
	}
	if input.Resources.LoadBalancer.ClosingIdleTimeout != nil ||
		input.Resources.LoadBalancer.ResetIdleTimeout != nil {
		t.Fatal("normalization changed omitted idle limits")
	}

	// Native documents retain integers above the exact float64 range and explicit zeroes.
	for _, limits := range [][2]time.Duration{{0, 9007199254740993}, {9007199254740995, 0}, {1, 2}} {
		input.Resources.LoadBalancer.ClosingIdleTimeout = &limits[0]
		input.Resources.LoadBalancer.ResetIdleTimeout = &limits[1]
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
		if resolved.LoadBalancer.ClosingIdleTimeout != limits[0] ||
			resolved.LoadBalancer.ResetIdleTimeout != limits[1] {
			t.Fatalf("LB idle limits changed: %+v", resolved.LoadBalancer)
		}
	}
}

func TestLBClosingAndResetIdleTimeoutsRejectNegativeDurations(t *testing.T) {
	for _, phase := range []string{"closing", "reset"} {
		t.Run(phase, func(t *testing.T) {
			input := exampleScenario()
			negative := -time.Nanosecond
			if phase == "closing" {
				input.Resources.LoadBalancer.ClosingIdleTimeout = &negative
			} else {
				input.Resources.LoadBalancer.ResetIdleTimeout = &negative
			}
			_, err := scenario.Normalize(input)
			path := "resources.loadBalancer." + phase + "IdleTimeoutNanoseconds"
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected %s error, got %v", path, err)
			}
		})
	}
}
