package scenario_test

import (
	"bytes"
	"strings"
	"testing"

	"load-balancer/scenario"
)

// Existing documents keep SACK off; an explicit false remains an explicit choice.
func TestSACKDefaultsAndDocumentRoundTrip(t *testing.T) {
	for _, requested := range []*bool{nil, pointer(false), pointer(true)} {
		input := exampleScenario()
		input.Fixed.TCP.SACKEnabled = requested

		encoded, err := scenario.EncodeDocumentV2(input)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := scenario.DecodeDocumentV2(encoded)
		if err != nil {
			t.Fatal(err)
		}

		// Omission inherits false without adding an override to the document.
		if requested == nil {
			if restored.Fixed.TCP.SACKEnabled != nil || bytes.Contains(encoded, []byte("sackEnabled")) {
				t.Fatal("an omitted SACK setting became an explicit override")
			}
		} else if restored.Fixed.TCP.SACKEnabled == nil || *restored.Fixed.TCP.SACKEnabled != *requested {
			t.Fatal("the explicit SACK choice changed during the round trip")
		}

		resolved, err := scenario.NormalizeV2(restored)
		if err != nil {
			t.Fatal(err)
		}
		want := requested != nil && *requested
		if resolved.Fixed.TCP.SACKEnabled != want {
			t.Fatalf("SACK enabled = %v, want %v", resolved.Fixed.TCP.SACKEnabled, want)
		}
	}
}

// SACK uses one recovery procedure; incompatible profiles are rejected explicitly.
func TestSACKSupportedRecoveryProfiles(t *testing.T) {
	for _, mode := range []string{"reno", "newReno", "cubic", "tahoe", "none"} {
		t.Run(mode, func(t *testing.T) {
			input := exampleScenario()
			input.Fixed.TCP.SACKEnabled = pointer(true)
			input.Fixed.TCP.CongestionControl.Mode = pointer(mode)

			_, err := scenario.NormalizeV2(input)
			wantValid := mode == "reno" || mode == "newReno" || mode == "cubic"
			if wantValid {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "tcp.sackEnabled") {
				t.Fatalf("expected an unsupported SACK profile error, got %v", err)
			}
		})
	}

	// Disabling fast recovery cannot silently retain SACK recovery.
	input := exampleScenario()
	input.Fixed.TCP.SACKEnabled = pointer(true)
	input.Fixed.TCP.CongestionControl.FastRetransmit = pointer(false)
	if _, err := scenario.NormalizeV2(input); err == nil || !strings.Contains(err.Error(), "tcp.sackEnabled") {
		t.Fatalf("expected a SACK dependency error, got %v", err)
	}
}
