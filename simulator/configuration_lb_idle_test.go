package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
)

func TestLBClosingAndResetIdleConfiguration(t *testing.T) {
	defaults := legacyTestConfiguration()
	prepared, err := prepareStart(startRequest{legacy: &runtimeCommand{Payload: []byte(`{}`)}}, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.config.LBClosingIdleTimeoutMs != 120000 || prepared.config.LBResetIdleTimeoutMs != 10000 {
		t.Fatalf("legacy LB idle defaults = %+v", prepared.config)
	}

	for _, limits := range [][2]float64{{0, 12.345678}, {3.456789, 0}} {
		requested := defaults.publicConfig()
		requested.Bytes = 64
		requested.LBClosingIdleTimeoutMs, requested.LBResetIdleTimeoutMs = limits[0], limits[1]
		runtime, err := configurationFromPublic(requested, defaults)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.publicConfig() != requested || defaults.lbClosingIdleTimeout != nil || defaults.lbResetIdleTimeout != nil {
			t.Fatal("LB idle conversion changed settings or process defaults")
		}
		resolved, err := scenario.Normalize(runtime.scenarioDefinition())
		if err != nil {
			t.Fatal(err)
		}
		summary := scenarioSummary(resolved)
		if summary.LBClosingIdleTimeoutMs != limits[0] || summary.LBResetIdleTimeoutMs != limits[1] {
			t.Fatalf("scenario summary lost LB idle limits: %+v", summary)
		}
	}
}

func TestLBClosingAndResetIdleConfigurationRejectsUnsafeDurations(t *testing.T) {
	for _, field := range []string{"lbClosingIdleTimeoutMs", "lbResetIdleTimeoutMs"} {
		for _, value := range []float64{-1, math.NaN(), math.Inf(1), float64(math.MaxInt64) / float64(time.Millisecond)} {
			defaults := legacyTestConfiguration()
			requested := defaults.publicConfig()
			if field == "lbClosingIdleTimeoutMs" {
				requested.LBClosingIdleTimeoutMs = value
			} else {
				requested.LBResetIdleTimeoutMs = value
			}
			_, err := configurationFromPublic(requested, defaults)
			if err == nil || !strings.Contains(err.Error(), field) {
				t.Fatalf("unsafe %s = %v: %v", field, value, err)
			}
		}
	}
}

func TestLBClosingAndResetIdleCLIFlags(t *testing.T) {
	config, err := readTestConfiguration(t, "-lb-closing-idle-timeout", "0", "-lb-reset-idle-timeout", "7ns")
	if err != nil {
		t.Fatal(err)
	}
	closing, reset := config.effectiveLBFinalIdleTimeouts()
	if closing != 0 || reset != 7*time.Nanosecond {
		t.Fatalf("LB CLI idle limits = %s, %s", closing, reset)
	}
	for _, flag := range []string{"-lb-closing-idle-timeout", "-lb-reset-idle-timeout"} {
		if _, err := readTestConfiguration(t, flag, "-1ns"); err == nil {
			t.Fatalf("negative timeout accepted for %s", flag)
		}
	}
}
