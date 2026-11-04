package scenario_test

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"load-balancer/scenario"
)

// The scenario uses the requested ECNenabled spelling and keeps omitted/false distinct.
func TestECNConfigurationAndDocument(t *testing.T) {
	for _, enabled := range []*bool{nil, pointer(false), pointer(true)} {
		input := exampleScenario()
		input.TCP.ECNEnabled = enabled
		data, err := scenario.EncodeDocument(input)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := scenario.DecodeDocument(data)
		if err != nil {
			t.Fatal(err)
		}
		if enabled != nil && (!bytes.Contains(data, []byte("ECNenabled")) ||
			restored.TCP.ECNEnabled == nil || *restored.TCP.ECNEnabled != *enabled) {
			t.Fatal("document changed the explicit ECN setting")
		}
		resolved, err := scenario.Normalize(restored)
		if err != nil || resolved.TCP.ECNEnabled != (enabled != nil && *enabled) {
			t.Fatalf("ECN normalization = %v, err=%v", resolved.TCP.ECNEnabled, err)
		}
	}

	// A sender advertising ECN must have an algorithm that can reduce its window.
	input := exampleScenario()
	input.TCP.ECNEnabled = pointer(true)
	input.TCP.CongestionControl.Mode = pointer("none")
	if _, err := scenario.Normalize(input); err == nil || !strings.Contains(err.Error(), "tcp.ECNenabled") {
		t.Fatalf("ECN without congestion control: %v", err)
	}
}

// Profiles inherit queue settings, while an explicit zero disables signal selection.
func TestCongestionPolicyInheritancePreservesZeroProbability(t *testing.T) {
	input := exampleScenario()
	input.Network.Defaults.QueuePolicy = "threshold"
	input.Network.Defaults.CongestionThresholdPercent = 80
	input.Network.Defaults.CongestionSignalProbability = 0.25
	input.Network.Profiles = []scenario.LinkProfile{{
		ID:     "early",
		Config: scenario.LinkPatch{CongestionThresholdPercent: pointer(25.0)},
	}}
	input.Resources.Backends[0].Network = scenario.EndpointNetworkConfig{
		Profile:          "early",
		FromLoadBalancer: scenario.LinkPatch{CongestionSignalProbability: pointer(0.0)},
	}
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	links := resolved.Backends[0].Network
	if links.ToLoadBalancer.QueuePolicy != "threshold" || links.ToLoadBalancer.CongestionThresholdPercent != 25 ||
		links.ToLoadBalancer.CongestionSignalProbability != 0.25 || links.FromLoadBalancer.CongestionSignalProbability != 0 {
		t.Fatalf("inherited queue settings = %+v", links)
	}
	if input.Network.Defaults.CongestionThresholdPercent != 80 || input.Network.Defaults.CongestionSignalProbability != 0.25 {
		t.Fatal("normalization changed the input defaults")
	}
}

// Turning signaling off with p=0 must survive validation, saving and reloading.
func TestQueuePolicyZeroSettingsSurviveDocumentRoundTrip(t *testing.T) {
	input := exampleScenario()
	input.Network.Defaults.CongestionThresholdPercent = 0
	input.Network.Defaults.CongestionSignalProbability = 0
	input.Resources.LoadBalancer.Ingress.CongestionThresholdPercent = 0
	input.Resources.LoadBalancer.Ingress.CongestionSignalProbability = 0

	data, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte(`"congestionThresholdPercent":0`)) != 2 ||
		bytes.Count(data, []byte(`"congestionSignalProbability":0`)) != 2 {
		t.Fatalf("explicit zero settings disappeared from the saved scenario: %s", data)
	}

	restored, err := scenario.DecodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Network.Defaults.CongestionSignalProbability != 0 ||
		restored.Resources.LoadBalancer.Ingress.CongestionSignalProbability != 0 {
		t.Fatal("reloading changed the disabled signal probability")
	}
}

// Reject unknown policies and unsafe numeric values before constructing queues.
func TestCongestionPolicyValidation(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*scenario.LinkConfig)
	}{
		{"policy", func(link *scenario.LinkConfig) { link.QueuePolicy = "RED" }},
		{"negative threshold", func(link *scenario.LinkConfig) { link.CongestionThresholdPercent = -1 }},
		{"threshold above capacity", func(link *scenario.LinkConfig) { link.CongestionThresholdPercent = 101 }},
		{"nonfinite threshold", func(link *scenario.LinkConfig) { link.CongestionThresholdPercent = math.NaN() }},
		{"negative probability", func(link *scenario.LinkConfig) { link.CongestionSignalProbability = -0.1 }},
		{"probability above one", func(link *scenario.LinkConfig) { link.CongestionSignalProbability = 1.1 }},
		{"nonfinite probability", func(link *scenario.LinkConfig) { link.CongestionSignalProbability = math.Inf(1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			test.edit(&input.Network.Defaults)
			if _, err := scenario.Normalize(input); err == nil {
				t.Fatal("invalid queue policy was accepted")
			}
		})
	}
}
