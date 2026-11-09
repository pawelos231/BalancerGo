package main

import (
	"testing"
	"time"
)

func TestCloseCLIFlags(t *testing.T) {
	cfg, err := readTestConfiguration(t, "-tcp-msl", "10ms", "-tcp-fin-wait-timeout", "2s", "-tcp-fin-retries", "0")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.effectiveCloseConfig(); got.MSL != 10*time.Millisecond || got.FinWaitTimeout != 2*time.Second || got.MaxFINRetries != 0 {
		t.Fatalf("CLI lost close settings: %+v", got)
	}
	for _, arguments := range [][]string{
		{"-tcp-msl", "0"}, {"-tcp-msl", "-1s"}, {"-tcp-msl", "1281024h"},
		{"-tcp-fin-wait-timeout", "0"}, {"-tcp-fin-retries", "-1"}, {"-tcp-fin-retries", "31"},
	} {
		if _, err := readTestConfiguration(t, arguments...); err == nil {
			t.Fatalf("invalid close flags accepted: %v", arguments)
		}
	}
}
