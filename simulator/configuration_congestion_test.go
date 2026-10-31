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
		requested.InitialSlowStartThresholdBytes != 65535 || !requested.FastRetransmit {
		t.Fatal("older configurations lost the shared congestion-control defaults")
	}

	// Preserve the baseline mode and a large exact threshold through the legacy adapter.
	requested.Bytes = 4096
	requested.CongestionControl = "none"
	requested.FastRetransmit = false
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
		summary.FastRetransmit != requested.FastRetransmit ||
		summary.InitialCongestionWindowSegments != requested.InitialCongestionWindowSegments ||
		summary.InitialSlowStartThresholdBytes != requested.InitialSlowStartThresholdBytes {
		t.Fatal("scenario summary lost congestion settings")
	}
}

// A legacy Reno run inherits fast recovery; an explicit false survives the form.
func TestConfigurationFastRetransmitRoundTrip(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	requested.FastRetransmit = false

	runtime, err := configurationFromPublic(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.effectiveCongestionConfig().FastRetransmit || runtime.publicConfig().FastRetransmit {
		t.Fatal("the explicit false was replaced by the Reno default")
	}
	resolved, err := scenario.Normalize(runtime.scenarioDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TCP.CongestionControl.FastRetransmit || scenarioSummary(resolved).FastRetransmit {
		t.Fatal("the scenario lost the disabled recovery policy")
	}

	// Mode none cannot request Reno's recovery rules through the legacy API.
	requested.CongestionControl = "none"
	requested.FastRetransmit = true
	if _, err := configurationFromPublic(requested, defaults); err == nil {
		t.Fatal("fast retransmit was accepted without Reno")
	}
}

// Omitting the flag follows the selected mode; explicitly enabling it requires Reno.
func TestFastRetransmitCLIFlag(t *testing.T) {
	tests := []struct {
		name    string
		flags   []string
		enabled bool
	}{
		{name: "Reno default", enabled: true},
		{name: "explicit disable", flags: []string{"-tcp-fast-retransmit=false"}},
		{name: "none default", flags: []string{"-tcp-congestion-control", "none"}},
		{
			name:  "none explicit disable",
			flags: []string{"-tcp-congestion-control", "none", "-tcp-fast-retransmit=false"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := readTestConfiguration(t, test.flags...)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.effectiveCongestionConfig().FastRetransmit != test.enabled {
				t.Fatalf("fast retransmit = %v, want %v", cfg.congestion.FastRetransmit, test.enabled)
			}
		})
	}

	// Flag order does not turn an incompatible explicit choice into a default.
	for _, arguments := range [][]string{
		{"-tcp-congestion-control", "none", "-tcp-fast-retransmit=true"},
		{"-tcp-fast-retransmit=true", "-tcp-congestion-control", "none"},
	} {
		if _, err := readTestConfiguration(t, arguments...); err == nil {
			t.Fatalf("incompatible recovery flags accepted: %v", arguments)
		}
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
