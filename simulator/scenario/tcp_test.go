package scenario

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"load-balancer/model"
)

func TestTCPReceiveBufferDefaultsAndLimits(t *testing.T) {
	// Existing documents omit TCP settings and retain the default capacity.
	input := exampleScenario()
	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TCP.ReceiveBufferBytes != model.DefaultReceiveBufferBytes {
		t.Fatalf("default receive buffer = %d", resolved.TCP.ReceiveBufferBytes)
	}
	if input.TCP.ReceiveBufferBytes != nil {
		t.Fatal("normalization changed the requested settings")
	}

	// A tiny positive buffer is valid; zero must not become an unlimited buffer.
	for _, capacity := range []int{1, 64, 65535, 0, -1, 65536} {
		input.TCP.ReceiveBufferBytes = pointer(capacity)
		resolved, err := Normalize(input)
		valid := capacity >= 1 && capacity <= 65535
		if !valid {
			if err == nil || !strings.Contains(err.Error(), "tcp.receiveBufferBytes") {
				t.Fatalf("capacity %d: expected receive-buffer error, got %v", capacity, err)
			}
			continue
		}

		if err != nil || resolved.TCP.ReceiveBufferBytes != capacity {
			t.Fatalf("capacity %d: got %+v, error %v", capacity, resolved.TCP, err)
		}
	}
}

func TestTCPTimerDefaultsAndExplicitOverrides(t *testing.T) {
	// Older scenarios inherit adaptive RTO: initially 1 s, bounded to 200 ms..60 s.
	input := exampleScenario()
	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	defaults := model.DefaultRTOConfig()
	if resolved.TCP.RTO.Mode != string(defaults.Mode) ||
		!resolved.TCP.RTO.ExponentialBackoff ||
		resolved.TCP.RTO.InitialTimeout != defaults.InitialTimeout ||
		resolved.TCP.RTO.MinimumTimeout != defaults.MinimumTimeout ||
		resolved.TCP.RTO.MaximumTimeout != defaults.MaximumTimeout {
		t.Fatalf("unexpected RTO defaults: %+v", resolved.TCP.RTO)
	}
	if resolved.TCP.ClientIdleTimeout != 20*time.Second ||
		resolved.LoadBalancer.HalfOpenIdleTimeout != 5*time.Second ||
		resolved.LoadBalancer.EstablishedIdleTimeout != 50*time.Second {
		t.Fatal("older scenarios lost their idle cleanup defaults")
	}

	// Fixed 200-ms retries, no backoff and no idle limits are valid independent choices.
	input.TCP.RTO = RTOConfig{
		Mode:               pointer("fixed"),
		ExponentialBackoff: pointer(false),
		InitialTimeout:     pointer(200 * time.Millisecond),
		MinimumTimeout:     pointer(100 * time.Millisecond),
		MaximumTimeout:     pointer(10 * time.Second),
	}
	input.TCP.ClientIdleTimeout = pointer(time.Duration(0))
	input.Resources.LoadBalancer.HalfOpenIdleTimeout = pointer(time.Duration(0))
	input.Resources.LoadBalancer.EstablishedIdleTimeout = pointer(time.Duration(0))

	// Import/export must preserve false and zero rather than replace them with defaults.
	document, err := EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = Normalize(restored)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TCP.RTO.Mode != "fixed" || resolved.TCP.RTO.ExponentialBackoff ||
		resolved.TCP.RTO.InitialTimeout != 200*time.Millisecond ||
		resolved.TCP.RTO.MinimumTimeout != 100*time.Millisecond ||
		resolved.TCP.RTO.MaximumTimeout != 10*time.Second {
		t.Fatalf("timer overrides changed: %+v", resolved.TCP.RTO)
	}
	if resolved.TCP.ClientIdleTimeout != 0 || resolved.LoadBalancer.HalfOpenIdleTimeout != 0 ||
		resolved.LoadBalancer.EstablishedIdleTimeout != 0 {
		t.Fatal("explicitly disabled idle timers were restored")
	}
}

func TestTCPTimerValidation(t *testing.T) {
	// Invalid timers cannot reach the engine; zero is allowed only for idle cleanup.
	cases := []struct {
		name   string
		change func(*Scenario)
		path   string
	}{
		{
			name:   "unknown mode",
			change: func(s *Scenario) { s.TCP.RTO.Mode = pointer("fast") },
			path:   "tcp.rto.mode",
		},
		{
			name:   "empty mode",
			change: func(s *Scenario) { s.TCP.RTO.Mode = pointer("") },
			path:   "tcp.rto.mode",
		},
		{
			name:   "zero initial",
			change: func(s *Scenario) { s.TCP.RTO.InitialTimeout = pointer(time.Duration(0)) },
			path:   "tcp.rto",
		},
		{
			name:   "negative minimum",
			change: func(s *Scenario) { s.TCP.RTO.MinimumTimeout = pointer(-time.Second) },
			path:   "tcp.rto",
		},
		{
			name:   "zero maximum",
			change: func(s *Scenario) { s.TCP.RTO.MaximumTimeout = pointer(time.Duration(0)) },
			path:   "tcp.rto",
		},
		{
			name:   "minimum above initial",
			change: func(s *Scenario) { s.TCP.RTO.MinimumTimeout = pointer(2 * time.Second) },
			path:   "tcp.rto",
		},
		{
			name:   "initial above maximum",
			change: func(s *Scenario) { s.TCP.RTO.InitialTimeout = pointer(61 * time.Second) },
			path:   "tcp.rto",
		},
		{
			name:   "negative client idle",
			change: func(s *Scenario) { s.TCP.ClientIdleTimeout = pointer(-time.Second) },
			path:   "tcp.clientIdle",
		},
		{
			name: "negative half-open idle",
			change: func(s *Scenario) {
				s.Resources.LoadBalancer.HalfOpenIdleTimeout = pointer(-time.Second)
			},
			path: "loadBalancer.halfOpen",
		},
		{
			name: "negative established idle",
			change: func(s *Scenario) {
				s.Resources.LoadBalancer.EstablishedIdleTimeout = pointer(-time.Second)
			},
			path: "loadBalancer.established",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			test.change(&input)
			_, err := Normalize(input)
			if err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %s error, got %v", test.path, err)
			}
		})
	}
}

func TestTCPReceiveBufferSurvivesDocumentRoundTrip(t *testing.T) {
	input := exampleScenario()
	input.TCP.ReceiveBufferBytes = pointer(64)

	document, err := EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if restored.TCP.ReceiveBufferBytes == nil || *restored.TCP.ReceiveBufferBytes != 64 {
		t.Fatal("the requested receive buffer did not survive import/export")
	}

	// The resolved view exposes the effective value rather than an optional patch.
	resolved, err := Normalize(restored)
	if err != nil {
		t.Fatal(err)
	}
	document, err = EncodeNormalizedDocument(resolved)
	if err != nil {
		t.Fatal(err)
	}
	var object struct {
		TCP NormalizedTCPConfig `json:"tcp"`
	}
	if err := json.Unmarshal(document, &object); err != nil {
		t.Fatal(err)
	}
	if object.TCP.ReceiveBufferBytes != 64 {
		t.Fatal("the resolved document omitted the effective receive buffer")
	}
}
