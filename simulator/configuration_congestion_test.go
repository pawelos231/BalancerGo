package main

import (
	"testing"

	"load-balancer/model"
)

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
