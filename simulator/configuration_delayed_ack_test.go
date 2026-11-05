package main

import (
	"testing"
	"time"
)

// The CLI preserves default immediate ACKs and can pin a one-segment cwnd experiment.
func TestDelayedACKCLIAndPublicConfiguration(t *testing.T) {
	defaults, err := readTestConfiguration(t)
	if err != nil {
		t.Fatal(err)
	}
	policy := defaults.effectiveDelayedACKConfig()
	if policy.Enabled || policy.SegmentsPerACK != 2 || policy.MaxDelay != 40*time.Millisecond {
		t.Fatalf("unexpected CLI defaults: %+v", policy)
	}
	configured, err := readTestConfiguration(t,
		"-tcp-delayed-ack", "-tcp-delayed-ack-segments", "2",
		"-tcp-delayed-ack-max-delay", "40.000001ms", "-tcp-maximum-window", "1",
	)
	if err != nil {
		t.Fatal(err)
	}
	requested := configured.publicConfig()
	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	resolved := prepared.resolved.TCP
	if !resolved.DelayedACK.Enabled || resolved.DelayedACK.SegmentsPerACK != 2 ||
		resolved.DelayedACK.MaxDelay != 40*time.Millisecond+time.Nanosecond ||
		resolved.CongestionControl.MaximumWindowSegments != 1 {
		t.Fatalf("configuration lost ACK timing or cwnd cap: %+v", resolved)
	}
	summary := scenarioSummary(prepared.resolved)
	if !summary.DelayedACKEnabled || summary.DelayedACKSegments != 2 ||
		summary.DelayedACKMaxDelayMs != requested.DelayedACKMaxDelayMs ||
		summary.MaximumCongestionWindowSegments != 1 {
		t.Fatal("form summary lost ACK timing or cwnd cap")
	}

	// Explicit zero timer and an unsupported segment threshold are rejected.
	for _, flags := range [][]string{
		{"-tcp-delayed-ack-max-delay", "0"},
		{"-tcp-delayed-ack-max-delay", "501ms"},
		{"-tcp-delayed-ack-segments", "3"},
		{"-tcp-maximum-window", "-1"},
		{"-tcp-congestion-control", "none", "-tcp-maximum-window", "1"},
	} {
		if _, err := readTestConfiguration(t, flags...); err == nil {
			t.Fatalf("invalid settings accepted: %v", flags)
		}
	}
}
