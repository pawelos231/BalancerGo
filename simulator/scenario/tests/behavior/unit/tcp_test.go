package scenario_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

func TestTCPReceiveBufferDefaultsAndLimits(t *testing.T) {
	// Existing documents omit TCP settings and retain the default capacity.
	input := exampleScenario()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fixed.TCP.ReceiveBufferBytes != model.DefaultReceiveBufferBytes {
		t.Fatalf("default receive buffer = %d", resolved.Fixed.TCP.ReceiveBufferBytes)
	}
	if input.Fixed.TCP.ReceiveBufferBytes != nil {
		t.Fatal("normalization changed the requested settings")
	}

	// A tiny positive buffer is valid; zero must not become an unlimited buffer.
	for _, capacity := range []int{1, 64, 65535, 0, -1, 65536} {
		input.Fixed.TCP.ReceiveBufferBytes = pointer(capacity)
		resolved, err := scenario.Normalize(input)
		valid := capacity >= 1 && capacity <= 65535
		if !valid {
			if err == nil || !strings.Contains(err.Error(), "tcp.receiveBufferBytes") {
				t.Fatalf("capacity %d: expected receive-buffer error, got %v", capacity, err)
			}
			continue
		}

		if err != nil || resolved.Fixed.TCP.ReceiveBufferBytes != capacity {
			t.Fatalf("capacity %d: got %+v, error %v", capacity, resolved.Fixed.TCP, err)
		}
	}
}

func TestTCPTimerDefaultsAndExplicitOverrides(t *testing.T) {
	// Older scenarios inherit adaptive RTO: initially 1 s, bounded to 200 ms..60 s.
	input := exampleScenario()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	defaults := model.DefaultRTOConfig()
	if resolved.Fixed.TCP.RTO.Mode != string(defaults.Mode) ||
		!resolved.Fixed.TCP.RTO.ExponentialBackoff ||
		resolved.Fixed.TCP.RTO.InitialTimeout != defaults.InitialTimeout ||
		resolved.Fixed.TCP.RTO.MinimumTimeout != defaults.MinimumTimeout ||
		resolved.Fixed.TCP.RTO.MaximumTimeout != defaults.MaximumTimeout {
		t.Fatalf("unexpected RTO defaults: %+v", resolved.Fixed.TCP.RTO)
	}
	if resolved.Fixed.TCP.ClientIdleTimeout != 20*time.Second ||
		resolved.States[0].LoadBalancer.HalfOpenIdleTimeout != 5*time.Second ||
		resolved.States[0].LoadBalancer.EstablishedIdleTimeout != 50*time.Second {
		t.Fatal("older scenarios lost their idle cleanup defaults")
	}

	// Fixed 200-ms retries, no backoff and no idle limits are valid independent choices.
	input.Fixed.TCP.RTO = scenario.RTOConfig{
		Mode:               pointer("fixed"),
		ExponentialBackoff: pointer(false),
		InitialTimeout:     pointer(200 * time.Millisecond),
		MinimumTimeout:     pointer(100 * time.Millisecond),
		MaximumTimeout:     pointer(10 * time.Second),
	}
	input.Fixed.TCP.ClientIdleTimeout = pointer(time.Duration(0))
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.MappingTimeouts.HalfOpenIdleTimeout = pointer(time.Duration(0))
	})
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.MappingTimeouts.EstablishedIdleTimeout = pointer(time.Duration(0))
	})

	// Import/export must preserve false and zero rather than replace them with defaults.
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
	if resolved.Fixed.TCP.RTO.Mode != "fixed" || resolved.Fixed.TCP.RTO.ExponentialBackoff ||
		resolved.Fixed.TCP.RTO.InitialTimeout != 200*time.Millisecond ||
		resolved.Fixed.TCP.RTO.MinimumTimeout != 100*time.Millisecond ||
		resolved.Fixed.TCP.RTO.MaximumTimeout != 10*time.Second {
		t.Fatalf("timer overrides changed: %+v", resolved.Fixed.TCP.RTO)
	}
	if resolved.Fixed.TCP.ClientIdleTimeout != 0 || resolved.States[0].LoadBalancer.HalfOpenIdleTimeout != 0 ||
		resolved.States[0].LoadBalancer.EstablishedIdleTimeout != 0 {
		t.Fatal("explicitly disabled idle timers were restored")
	}
}

func TestTCPTimerValidation(t *testing.T) {
	// Invalid timers cannot reach the engine; zero is allowed only for idle cleanup.
	cases := []struct {
		name   string
		change func(*scenario.Scenario)
		path   string
	}{
		{
			name:   "unknown mode",
			change: func(s *scenario.Scenario) { s.Fixed.TCP.RTO.Mode = pointer("fast") },
			path:   "tcp.rto.mode",
		},
		{
			name:   "empty mode",
			change: func(s *scenario.Scenario) { s.Fixed.TCP.RTO.Mode = pointer("") },
			path:   "tcp.rto.mode",
		},
		{
			name:   "zero initial",
			change: func(s *scenario.Scenario) { s.Fixed.TCP.RTO.InitialTimeout = pointer(time.Duration(0)) },
			path:   "tcp.rto",
		},
		{
			name:   "negative minimum",
			change: func(s *scenario.Scenario) { s.Fixed.TCP.RTO.MinimumTimeout = pointer(-time.Second) },
			path:   "tcp.rto",
		},
		{
			name:   "zero maximum",
			change: func(s *scenario.Scenario) { s.Fixed.TCP.RTO.MaximumTimeout = pointer(time.Duration(0)) },
			path:   "tcp.rto",
		},
		{
			name:   "minimum above initial",
			change: func(s *scenario.Scenario) { s.Fixed.TCP.RTO.MinimumTimeout = pointer(2 * time.Second) },
			path:   "tcp.rto",
		},
		{
			name:   "initial above maximum",
			change: func(s *scenario.Scenario) { s.Fixed.TCP.RTO.InitialTimeout = pointer(61 * time.Second) },
			path:   "tcp.rto",
		},
		{
			name:   "negative client idle",
			change: func(s *scenario.Scenario) { s.Fixed.TCP.ClientIdleTimeout = pointer(-time.Second) },
			path:   "tcp.clientIdle",
		},
		{
			name: "negative half-open idle",
			change: func(s *scenario.Scenario) {
				editInitialState(s, func(state *scenario.StateConfig) {
					state.Resources.LoadBalancer.MappingTimeouts.HalfOpenIdleTimeout = pointer(-time.Second)
				})
			},
			path: "loadBalancer.mappingTimeouts.halfOpen",
		},
		{
			name: "negative established idle",
			change: func(s *scenario.Scenario) {
				editInitialState(s, func(state *scenario.StateConfig) {
					state.Resources.LoadBalancer.MappingTimeouts.EstablishedIdleTimeout = pointer(-time.Second)
				})
			},
			path: "loadBalancer.mappingTimeouts.established",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			test.change(&input)
			_, err := scenario.Normalize(input)
			if err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %s error, got %v", test.path, err)
			}
		})
	}
}

func TestTCPReceiveBufferSurvivesDocumentRoundTrip(t *testing.T) {
	input := exampleScenario()
	input.Fixed.TCP.ReceiveBufferBytes = pointer(64)

	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := scenario.DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Fixed.TCP.ReceiveBufferBytes == nil || *restored.Fixed.TCP.ReceiveBufferBytes != 64 {
		t.Fatal("the requested receive buffer did not survive import/export")
	}

	// The resolved view exposes the effective value rather than an optional patch.
	resolved, err := scenario.Normalize(restored)
	if err != nil {
		t.Fatal(err)
	}
	document, err = scenario.EncodeNormalizedDocument(resolved)
	if err != nil {
		t.Fatal(err)
	}
	var object struct {
		Fixed struct {
			TCP scenario.NormalizedTCPConfig `json:"tcp"`
		} `json:"fixed"`
	}
	if err := json.Unmarshal(document, &object); err != nil {
		t.Fatal(err)
	}
	if object.Fixed.TCP.ReceiveBufferBytes != 64 {
		t.Fatal("the resolved document omitted the effective receive buffer")
	}
}
