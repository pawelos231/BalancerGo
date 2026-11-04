package main

import (
	"testing"

	"load-balancer/model"
)

func TestConfigurationCongestionRoundTrip(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	if requested.CongestionControl != "reno" || requested.InitialCongestionWindowSegments != 4 ||
		requested.InitialSlowStartThresholdBytes != 65535 || !requested.FastRetransmit ||
		!requested.CubicFastConvergence {
		t.Fatal("older configurations lost the shared congestion-control defaults")
	}

	// Preserve the baseline mode and a large exact threshold through the legacy adapter.
	requested.Bytes = 4096
	requested.CongestionControl = "none"
	requested.FastRetransmit = false
	requested.InitialCongestionWindowSegments = 2
	requested.InitialSlowStartThresholdBytes = 9_007_199_254_740_993
	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	input := prepared.definition.TCP.CongestionControl
	if *input.Mode != "none" || *input.FastRetransmit || *input.InitialWindowSegments != 2 ||
		*input.InitialSlowStartThresholdBytes != requested.InitialSlowStartThresholdBytes {
		t.Fatalf("congestion settings changed in the retained input: %+v", input)
	}

	resolved := prepared.resolved
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

	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if *prepared.definition.TCP.CongestionControl.FastRetransmit {
		t.Fatal("the explicit false was replaced by the Reno default")
	}
	resolved := prepared.resolved
	if resolved.TCP.CongestionControl.FastRetransmit || scenarioSummary(resolved).FastRetransmit {
		t.Fatal("the scenario lost the disabled recovery policy")
	}

	// Mode none cannot request early loss detection through the legacy API.
	requested.CongestionControl = "none"
	requested.FastRetransmit = true
	if _, err := preparePublicScenario(requested, defaults); err == nil {
		t.Fatal("fast retransmit was accepted with congestion control disabled")
	}
}

// Omitting the retry flag follows the selected congestion mode.
func TestFastRetransmitCLIFlag(t *testing.T) {
	tests := []struct {
		name    string
		flags   []string
		enabled bool
	}{
		{name: "Reno default", enabled: true},
		{
			name:    "NewReno default",
			flags:   []string{"-tcp-congestion-control", "newReno"},
			enabled: true,
		},
		{
			name:  "NewReno explicit disable",
			flags: []string{"-tcp-congestion-control", "newReno", "-tcp-fast-retransmit=false"},
		},
		{
			name:    "NewReno explicit enable before mode",
			flags:   []string{"-tcp-fast-retransmit=true", "-tcp-congestion-control", "newReno"},
			enabled: true,
		},
		{
			name:    "CUBIC default",
			flags:   []string{"-tcp-congestion-control", "cubic"},
			enabled: true,
		},
		{
			name:  "CUBIC explicit disable",
			flags: []string{"-tcp-congestion-control", "cubic", "-tcp-fast-retransmit=false"},
		},
		{
			name:    "CUBIC explicit enable before mode",
			flags:   []string{"-tcp-fast-retransmit=true", "-tcp-congestion-control", "cubic"},
			enabled: true,
		},
		{
			name:    "Tahoe default",
			flags:   []string{"-tcp-congestion-control", "tahoe"},
			enabled: true,
		},
		{
			name:  "Tahoe explicit disable",
			flags: []string{"-tcp-congestion-control", "tahoe", "-tcp-fast-retransmit=false"},
		},
		{
			name:    "Tahoe explicit enable before mode",
			flags:   []string{"-tcp-fast-retransmit=true", "-tcp-congestion-control", "tahoe"},
			enabled: true,
		},
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

// Form edits and the native scenario retain each mode and retry flag.
func TestConfigurationRecoveryModesRoundTrip(t *testing.T) {
	for _, mode := range []string{"tahoe", "newReno"} {
		t.Run(mode, func(t *testing.T) {
			for _, fastRetransmit := range []bool{true, false} {
				defaults := legacyTestConfiguration()
				requested := defaults.publicConfig()
				requested.Bytes = 4096
				requested.CongestionControl = mode
				requested.FastRetransmit = fastRetransmit

				prepared, err := preparePublicScenario(requested, defaults)
				if err != nil {
					t.Fatal(err)
				}
				input := prepared.definition.TCP.CongestionControl
				if *input.Mode != mode || *input.FastRetransmit != fastRetransmit {
					t.Fatalf("%s settings changed in the retained input: %+v", mode, input)
				}

				resolved := prepared.resolved
				config := resolved.TCP.CongestionControl
				if config.Mode != mode || config.FastRetransmit != fastRetransmit {
					t.Fatalf("native scenario lost the %s profile: %+v", mode, config)
				}
				summary := scenarioSummary(resolved)
				if summary.CongestionControl != mode || summary.FastRetransmit != fastRetransmit {
					t.Fatalf("scenario summary lost the %s profile: %+v", mode, summary)
				}
			}
		})
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
		{"-tcp-congestion-control", "bbr"},
		{"-tcp-initial-window", "0"},
		{"-tcp-slow-start-threshold", "2047"},
	} {
		if _, err := readTestConfiguration(t, arguments...); err == nil {
			t.Fatalf("invalid congestion flags accepted: %v", arguments)
		}
	}
}

// CUBIC settings use the same native/flat paths, including disabled fast convergence.
func TestConfigurationCubicRoundTrip(t *testing.T) {
	for _, fastConvergence := range []bool{true, false} {
		defaults := legacyTestConfiguration()
		requested := defaults.publicConfig()
		requested.Bytes = 4096
		requested.CongestionControl = "cubic"
		requested.CubicFastConvergence = fastConvergence

		prepared, err := preparePublicScenario(requested, defaults)
		if err != nil {
			t.Fatal(err)
		}
		input := prepared.definition.TCP.CongestionControl
		if *input.Mode != "cubic" || *input.CubicFastConvergence != fastConvergence {
			t.Fatalf("CUBIC settings changed in the retained input: %+v", input)
		}
		resolved := prepared.resolved
		config := resolved.TCP.CongestionControl
		if config.Mode != "cubic" || config.CubicFastConvergence != fastConvergence {
			t.Fatalf("native scenario lost CUBIC settings: %+v", config)
		}
		summary := scenarioSummary(resolved)
		if summary.CongestionControl != "cubic" || summary.CubicFastConvergence != fastConvergence {
			t.Fatalf("flat summary lost CUBIC settings: %+v", summary)
		}
	}
}

// A single-flow CLI experiment can disable convergence without changing early retry.
func TestCubicFastConvergenceCLIFlag(t *testing.T) {
	for _, arguments := range [][]string{
		{"-tcp-congestion-control", "cubic", "-tcp-cubic-fast-convergence=false"},
		{"-tcp-cubic-fast-convergence=false", "-tcp-congestion-control", "cubic"},
	} {
		cfg, err := readTestConfiguration(t, arguments...)
		if err != nil {
			t.Fatal(err)
		}
		config := cfg.effectiveCongestionConfig()
		if config.Mode != model.CongestionCubic || config.CubicFastConvergence || !config.FastRetransmit {
			t.Fatalf("unexpected CUBIC CLI settings: %+v", config)
		}
	}
}
