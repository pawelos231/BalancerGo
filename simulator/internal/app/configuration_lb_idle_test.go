package app

import (
	"testing"
	"time"
)

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
