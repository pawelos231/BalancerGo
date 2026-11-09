package scenario_test

import (
	"testing"
	"time"

	"load-balancer/scenario"
)

// Omitted settings keep immediate ACKs; explicit values survive the scenario document.
func TestDelayedACKDefaultsAndDocumentRoundTrip(t *testing.T) {
	input := exampleScenario()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fixed.TCP.DelayedACK.Enabled || resolved.Fixed.TCP.DelayedACK.SegmentsPerACK != 2 ||
		resolved.Fixed.TCP.DelayedACK.MaxDelay != 40*time.Millisecond {
		t.Fatalf("unexpected defaults: %+v", resolved.Fixed.TCP.DelayedACK)
	}

	input.Fixed.TCP.DelayedACK = scenario.DelayedACKConfig{
		Enabled:        pointer(true),
		SegmentsPerACK: pointer(1),
		MaxDelay:       pointer(40*time.Millisecond + time.Nanosecond),
	}
	data, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := scenario.DecodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = scenario.Normalize(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Fixed.TCP.DelayedACK.Enabled || resolved.Fixed.TCP.DelayedACK.SegmentsPerACK != 1 ||
		resolved.Fixed.TCP.DelayedACK.MaxDelay != 40*time.Millisecond+time.Nanosecond {
		t.Fatalf("delayed ACK settings changed during round trip: %+v", resolved.Fixed.TCP.DelayedACK)
	}
}

// Reject invalid thresholds and timers, including an explicit zero when disabled.
func TestDelayedACKValidation(t *testing.T) {
	for _, invalid := range []scenario.DelayedACKConfig{
		{SegmentsPerACK: pointer(0)},
		{SegmentsPerACK: pointer(3)},
		{MaxDelay: pointer(time.Duration(0))},
		{MaxDelay: pointer(500*time.Millisecond + time.Nanosecond)},
	} {
		input := exampleScenario()
		input.Fixed.TCP.DelayedACK = invalid
		if _, err := scenario.Normalize(input); err == nil {
			t.Fatalf("invalid ACK settings accepted: %+v", invalid)
		}
	}
}

// A one-segment maximum is valid even when the configured initial window is larger.
func TestMaximumCongestionWindowConfiguration(t *testing.T) {
	input := exampleScenario()
	input.Fixed.TCP.CongestionControl.MaximumWindowSegments = pointer(1)
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fixed.TCP.CongestionControl.MaximumWindowSegments != 1 {
		t.Fatal("maximum window was not retained")
	}

	input.Fixed.TCP.CongestionControl.MaximumWindowSegments = pointer(-1)
	if _, err := scenario.Normalize(input); err == nil {
		t.Fatal("negative maximum window accepted")
	}
	input.Fixed.TCP.CongestionControl.MaximumWindowSegments = pointer(1)
	input.Fixed.TCP.CongestionControl.Mode = pointer("none")
	input.Fixed.TCP.CongestionControl.FastRetransmit = pointer(false)
	if _, err := scenario.Normalize(input); err == nil {
		t.Fatal("maximum window accepted without congestion control")
	}
}
