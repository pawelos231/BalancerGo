package main

import (
	"load-balancer/scenario"
	"testing"
)

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
}
