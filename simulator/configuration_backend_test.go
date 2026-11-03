package main

import (
	"testing"
	"time"

	"load-balancer/scenario"
)

func TestBackendTimeoutConfigurationRoundTrip(t *testing.T) {
	// A legacy form gets the same omitted defaults as a native scenario.
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	if requested.BackendHalfOpenIdleTimeoutMs != 5000 ||
		requested.BackendEstablishedIdleTimeoutMs != 0 {
		t.Fatalf("backend defaults = %+v", requested)
	}

	// Preserve explicit zero and fractional milliseconds without changing process defaults.
	requested.Bytes = 64
	requested.BackendHalfOpenIdleTimeoutMs = 0
	requested.BackendEstablishedIdleTimeoutMs = 12.345678
	runtime, err := configurationFromPublic(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.publicConfig() != requested || defaults.backendHalfOpenIdleTimeout != nil ||
		defaults.backendEstablishedIdleTimeout != nil {
		t.Fatalf("backend timer conversion changed values or defaults:\n got %+v\nwant %+v", runtime.publicConfig(), requested)
	}

	resolved, err := scenario.Normalize(runtime.scenarioDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TCP.BackendHalfOpenIdleTimeout != 0 ||
		resolved.TCP.BackendEstablishedIdleTimeout != 12345678*time.Nanosecond {
		t.Fatalf("native backend timers = %+v", resolved.TCP)
	}
	summary := scenarioSummary(resolved)
	if summary.BackendHalfOpenIdleTimeoutMs != requested.BackendHalfOpenIdleTimeoutMs ||
		summary.BackendEstablishedIdleTimeoutMs != requested.BackendEstablishedIdleTimeoutMs {
		t.Fatal("scenario summary lost backend timer settings")
	}
}

func TestBackendTimeoutCLIFlags(t *testing.T) {
	// Configure both timers independently, including explicitly disabled cleanup.
	config, err := readTestConfiguration(t,
		"-backend-half-open-idle-timeout", "0",
		"-backend-established-idle-timeout", "7s",
	)
	if err != nil {
		t.Fatal(err)
	}
	halfOpen, established := config.effectiveBackendIdleTimeouts()
	if halfOpen != 0 || established != 7*time.Second {
		t.Fatalf("backend CLI timers = %s, %s", halfOpen, established)
	}

	for _, flag := range []string{
		"-backend-half-open-idle-timeout",
		"-backend-established-idle-timeout",
	} {
		if _, err := readTestConfiguration(t, flag, "-1ns"); err == nil {
			t.Fatalf("negative timeout accepted for %s", flag)
		}
	}
}
