package main

import (
	"testing"
	"time"
)

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
