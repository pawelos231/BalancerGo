package main

import (
	"math"
	"testing"

	"load-balancer/scenario"
)

func TestConfigurationECNAndQueuePolicyRoundTrip(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	if requested.ECNEnabled {
		t.Fatal("ECN must be opt-in")
	}
	requested.ECNEnabled = true
	requested.QueuePolicy = "threshold"
	requested.CongestionThresholdPercent = 80
	requested.CongestionSignalProbability = 1
	requested.IngressQueuePolicy = "threshold"
	// Explicit zero threshold and probability must not be replaced with defaults.
	requested.IngressCongestionThresholdPercent = 0
	requested.IngressCongestionSignalProbability = 0
	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	summary := scenarioSummary(prepared.resolved)
	if !*prepared.definition.TCP.ECNEnabled || !summary.ECNEnabled ||
		summary.QueuePolicy != "threshold" || summary.CongestionThresholdPercent != 80 ||
		summary.CongestionSignalProbability != 1 || summary.IngressQueuePolicy != "threshold" ||
		summary.IngressCongestionThresholdPercent != 0 || summary.IngressCongestionSignalProbability != 0 {
		t.Fatalf("ECN or queue settings were lost: %+v", summary)
	}
	requested.CongestionControl = "none"
	requested.FastRetransmit = false
	if _, err := preparePublicScenario(requested, defaults); err == nil {
		t.Fatal("ECN with mode none was accepted")
	}
}

func TestECNAndQueueCLIFlags(t *testing.T) {
	defaults, err := readTestConfiguration(t)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.ecnEnabled || defaults.queuePolicy != "tailDrop" ||
		defaults.congestionThresholdPercent != 80 || defaults.congestionSignalProbability != 1 ||
		defaults.ingress != scenario.DefaultIngressConfig() {
		t.Fatal("CLI defaults changed legacy ingress behavior or enabled congestion signaling")
	}
	configured, err := readTestConfiguration(t, "-ecn-enabled", "-queue-policy", "threshold",
		"-queue-threshold-percent", "25", "-queue-signal-probability", "0.5",
		"-lb-ingress-queue-policy", "threshold", "-lb-ingress-queue-threshold-percent", "0",
		"-lb-ingress-queue-signal-probability", "0")
	if err != nil {
		t.Fatal(err)
	}
	if !configured.ecnEnabled || configured.queuePolicy != "threshold" ||
		configured.congestionThresholdPercent != 25 || configured.congestionSignalProbability != 0.5 ||
		configured.ingress.QueuePolicy != "threshold" || configured.ingress.CongestionThresholdPercent != 0 ||
		configured.ingress.CongestionSignalProbability != 0 {
		t.Fatal("CLI flags lost ECN or queue settings")
	}
	for _, flags := range [][]string{
		{"-ecn-enabled", "-tcp-congestion-control", "none"},
		{"-queue-policy", "RED"}, {"-queue-threshold-percent", "100.1"},
		{"-queue-signal-probability", "NaN"}, {"-lb-ingress-queue-signal-probability", "1.1"},
	} {
		if _, err := readTestConfiguration(t, flags...); err == nil {
			t.Fatalf("accepted invalid flags %v", flags)
		}
	}
	requested := defaults.publicConfig()
	requested.CongestionThresholdPercent = math.Inf(1)
	if _, err := preparePublicScenario(requested, defaults); err == nil {
		t.Fatal("accepted infinite form threshold")
	}
}
