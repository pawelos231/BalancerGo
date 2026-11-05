package scenario_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
)

// Old scenarios keep instant handling; one backend can opt into CPU processing.
// Its zero queue accepts an idle core but provides no waiting slots.
func TestHostDefaultsAndIndependentBackendRoundTrip(t *testing.T) {
	input := exampleScenario()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	wantDefault := scenario.DefaultHostConfig()
	if resolved.LoadBalancer.Host != wantDefault || resolved.Backends[0].Host != wantDefault {
		t.Fatalf("omitted hosts lost the idealized default: %+v", resolved)
	}

	input.Resources.Backends[0].Host = scenario.HostConfig{
		Cores:                pointer(2),
		PacketProcessingTime: pointer(10 * time.Microsecond),
		QueueCapacity:        pointer(0),
	}
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
	backend := resolved.Backends[0].Host
	if backend.Cores != 2 || backend.PacketProcessingTime != 10*time.Microsecond || backend.QueueCapacity != 0 {
		t.Fatalf("backend host changed across JSON round-trip: %+v", backend)
	}
	if resolved.LoadBalancer.Host != wantDefault {
		t.Fatal("a backend CPU override changed the balancer")
	}
}

// Reject invalid limits with a resource path, including time-counter overflow.
func TestHostValidation(t *testing.T) {
	cases := []struct {
		name   string
		config scenario.HostConfig
	}{
		{name: "zero cores", config: scenario.HostConfig{Cores: pointer(0)}},
		{name: "too many cores", config: scenario.HostConfig{Cores: pointer(scenario.MaxHostCores + 1)}},
		{name: "negative time", config: scenario.HostConfig{PacketProcessingTime: pointer(-time.Nanosecond)}},
		{name: "negative queue", config: scenario.HostConfig{QueueCapacity: pointer(-1)}},
		{name: "too large queue", config: scenario.HostConfig{QueueCapacity: pointer(scenario.MaxHostQueueCapacity + 1)}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			input.Resources.Backends[0].Host = test.config
			_, err := scenario.Normalize(input)
			if err == nil || !strings.Contains(err.Error(), "resources.backends[0].host") {
				t.Fatalf("expected a backend host error, got %v", err)
			}
		})
	}

	// Two busy cores over this horizon would overflow the cumulative duration.
	input := exampleScenario()
	input.Execution.Duration = time.Duration(math.MaxInt64)
	input.Execution.Speed = 0
	input.Resources.LoadBalancer.Host = scenario.HostConfig{
		Cores:                pointer(2),
		PacketProcessingTime: pointer(time.Nanosecond),
		QueueCapacity:        pointer(0),
	}
	if _, err := scenario.Normalize(input); err == nil {
		t.Fatal("accepted CPU counters that cannot represent the experiment horizon")
	}
	input.Resources.LoadBalancer.Host.PacketProcessingTime = pointer(time.Duration(0))
	if _, err := scenario.Normalize(input); err != nil {
		t.Fatalf("disabled CPU must not reject unused time counters: %v", err)
	}
}
