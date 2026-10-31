package main

import (
	"testing"

	"load-balancer/model"
	"load-balancer/scenario"
)

func TestConfigurationCongestionRoundTrip(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	if requested.CongestionControl != "reno" || requested.InitialCongestionWindowSegments != 4 ||
		requested.InitialSlowStartThresholdBytes != 65535 {
		t.Fatal("older configurations lost the shared congestion-control defaults")
	}

	// Preserve the baseline mode and a large exact threshold through the legacy adapter.
	requested.Bytes = 4096
	requested.CongestionControl = "none"
	requested.InitialCongestionWindowSegments = 2
	requested.InitialSlowStartThresholdBytes = 9_007_199_254_740_993
	runtime, err := configurationFromPublic(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if restored := runtime.publicConfig(); restored != requested {
		t.Fatalf("congestion settings changed during the form round trip: %+v", restored)
	}

	resolved, err := scenario.Normalize(runtime.scenarioDefinition())
	if err != nil {
		t.Fatal(err)
	}
	congestion := resolved.TCP.CongestionControl
	if congestion.Mode != "none" || congestion.InitialWindowSegments != 2 ||
		congestion.InitialSlowStartThresholdBytes != requested.InitialSlowStartThresholdBytes {
		t.Fatalf("native scenario lost congestion settings: %+v", congestion)
	}
	summary := scenarioSummary(resolved)
	if summary.CongestionControl != requested.CongestionControl ||
		summary.InitialCongestionWindowSegments != requested.InitialCongestionWindowSegments ||
		summary.InitialSlowStartThresholdBytes != requested.InitialSlowStartThresholdBytes {
		t.Fatal("scenario summary lost congestion settings")
	}
}

func TestCongestionCLIFlags(t *testing.T) {
	// Mode none disables the sender window without changing the configured initial values.
	cfg, err := readTestConfiguration(t,
		"-tcp-congestion-control", "none",
		"-tcp-initial-window", "2",
		"-tcp-slow-start-threshold", "2048",
	)
	if err != nil {
		t.Fatal(err)
	}
	congestion := cfg.effectiveCongestionConfig()
	if congestion.Mode != model.CongestionNone || congestion.InitialWindowSegments != 2 ||
		congestion.InitialSlowStartThresholdBytes != 2048 {
		t.Fatalf("the CLI ignored congestion settings: %+v", congestion)
	}

	for _, arguments := range [][]string{
		{"-tcp-congestion-control", ""},
		{"-tcp-congestion-control", "cubic"},
		{"-tcp-initial-window", "0"},
		{"-tcp-slow-start-threshold", "2047"},
	} {
		if _, err := readTestConfiguration(t, arguments...); err == nil {
			t.Fatalf("invalid congestion flags accepted: %v", arguments)
		}
	}
}
