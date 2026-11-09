package main

import (
	"testing"
	"time"
)

// CLI CPU settings are retained in the scenario state, including a zero-slot FIFO.
func TestHostCLIProfileReachesNativeState(t *testing.T) {
	configured, err := readTestConfiguration(t,
		"-lb-cpu-cores", "2",
		"-lb-packet-processing-time", "10us",
		"-lb-cpu-queue", "0",
		"-backend-packet-processing-time", "20us",
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := configured.prepareScenario()
	if err != nil {
		t.Fatal(err)
	}
	state := initialScenarioState(t, prepared.plan)
	if state.LoadBalancer.Host.Cores != 2 || state.LoadBalancer.Host.PacketProcessingTime != 10*time.Microsecond ||
		state.LoadBalancer.Host.QueueCapacity != 0 {
		t.Fatalf("LB CPU settings changed: %+v", state.LoadBalancer.Host)
	}
	for _, backend := range state.Backends {
		if backend.Host.PacketProcessingTime != 20*time.Microsecond {
			t.Fatalf("backend CPU settings changed: %+v", backend.Host)
		}
	}
}
