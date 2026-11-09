package scenario_test

import (
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
)

func TestBackendTimeoutDefaultsAndDocumentOverrides(t *testing.T) {
	// Old documents expire incomplete handshakes after 5 s; established idle cleanup is off.
	input := exampleScenario()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fixed.TCP.BackendHalfOpenIdleTimeout != 5*time.Second ||
		resolved.Fixed.TCP.BackendEstablishedIdleTimeout != 0 {
		t.Fatalf("backend defaults = %+v", resolved.Fixed.TCP)
	}
	if input.Fixed.TCP.BackendHalfOpenIdleTimeout != nil || input.Fixed.TCP.BackendEstablishedIdleTimeout != nil {
		t.Fatal("normalization changed the requested document")
	}

	// Explicit zero disables the handshake limit; idle cleanup keeps nanosecond precision.
	input.Fixed.TCP.BackendHalfOpenIdleTimeout = pointer(time.Duration(0))
	input.Fixed.TCP.BackendEstablishedIdleTimeout = pointer(1234567 * time.Nanosecond)
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := scenario.DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = scenario.Normalize(restored)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fixed.TCP.BackendHalfOpenIdleTimeout != 0 ||
		resolved.Fixed.TCP.BackendEstablishedIdleTimeout != 1234567*time.Nanosecond {
		t.Fatalf("backend overrides = %+v", resolved.Fixed.TCP)
	}
}

func TestBackendTimeoutsRejectNegativeDurations(t *testing.T) {
	// Each error identifies the backend timer that cannot be scheduled.
	tests := []struct {
		path string
		set  func(*scenario.TCPConfig)
	}{
		{
			path: "tcp.backendHalfOpenIdleTimeoutNanoseconds",
			set: func(config *scenario.TCPConfig) {
				config.BackendHalfOpenIdleTimeout = pointer(-time.Nanosecond)
			},
		},
		{
			path: "tcp.backendEstablishedIdleTimeoutNanoseconds",
			set: func(config *scenario.TCPConfig) {
				config.BackendEstablishedIdleTimeout = pointer(-time.Nanosecond)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			input := exampleScenario()
			test.set(&input.Fixed.TCP)

			_, err := scenario.Normalize(input)
			if err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %s error, got %v", test.path, err)
			}
		})
	}
}
