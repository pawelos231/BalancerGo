package main

import (
	"testing"
	"time"
)

// CPU CLI flags, legacy form values and native scenarios retain the same limits.
func TestHostConfigurationRoundTrip(t *testing.T) {
	defaults, err := readTestConfiguration(t)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := readTestConfiguration(t,
		"-lb-cpu-cores", "2",
		"-lb-packet-processing-time", "10us",
		"-lb-cpu-queue", "0",
		"-backend-packet-processing-time", "20us",
	)
	if err != nil {
		t.Fatal(err)
	}
	public := configured.publicConfig()
	if public.LBHostCores != 2 || public.LBPacketProcessingTimeUs != 10 || public.LBCPUQueueCapacity != 0 {
		t.Fatalf("CLI host flags did not reach the form: %+v", public)
	}
	prepared, err := preparePublicScenario(public, defaults)
	if err != nil {
		t.Fatal(err)
	}
	lb := prepared.resolved.LoadBalancer.Host
	if lb.Cores != 2 || lb.PacketProcessingTime != 10*time.Microsecond || lb.QueueCapacity != 0 {
		t.Fatalf("form host settings did not reach the model: %+v", lb)
	}
	for _, backend := range prepared.resolved.Backends {
		if backend.Host.PacketProcessingTime != 20*time.Microsecond {
			t.Fatalf("shared backend CPU setting was lost: %+v", backend.Host)
		}
	}

	// An explicit form zero disables CPU instead of restoring the CLI's 10 us.
	public.LBPacketProcessingTimeUs = 0
	prepared, err = preparePublicScenario(public, configured)
	if err != nil || prepared.resolved.LoadBalancer.Host.PacketProcessingTime != 0 {
		t.Fatalf("zero did not disable processing: %v", err)
	}
	public.LBPacketProcessingTimeUs = -1
	if _, err := preparePublicScenario(public, defaults); err == nil {
		t.Fatal("negative processing time was accepted")
	}
	public.LBPacketProcessingTimeUs = 0.0001
	if _, err := preparePublicScenario(public, defaults); err == nil {
		t.Fatal("a positive CPU cost smaller than one nanosecond silently disabled processing")
	}
}
