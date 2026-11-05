package scenario_test

import (
	"math"
	"strings"
	"testing"

	"load-balancer/model"
	"load-balancer/scenario"
)

// Changing segmentation must not change five legacy payload units into more data.
func TestManualMSSPreservesPayloadUnitsAndRoundTrip(t *testing.T) {
	input := exampleScenario()
	input.Workload.Defaults.Packets = 5
	defaults, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.TCP.MSSBytes != model.DefaultMSSBytes {
		t.Fatalf("omitted MSS=%d, want 1024", defaults.TCP.MSSBytes)
	}

	for _, mss := range []int{536, 1024, 1452, 1460, 8960, 1234} {
		input.TCP.MSSBytes = pointer(mss)
		document, err := scenario.EncodeDocument(input)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := scenario.DecodeDocument(document)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := scenario.Normalize(restored)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.TCP.MSSBytes != mss || resolved.DefaultStream.Bytes != 5120 {
			t.Fatalf("MSS %d changed the requested payload: %+v", mss, resolved.DefaultStream)
		}
	}
}

// Explicit zero is invalid; a high MSS needs a correspondingly large byte threshold.
func TestManualMSSAndWindowValidation(t *testing.T) {
	for _, invalid := range []int{-1, 0, model.MaxMSSBytes + 1} {
		input := exampleScenario()
		input.TCP.MSSBytes = pointer(invalid)
		if _, err := scenario.Normalize(input); err == nil || !strings.Contains(err.Error(), "tcp.mssBytes") {
			t.Fatalf("MSS %d: expected an MSS error, got %v", invalid, err)
		}
	}

	input := exampleScenario()
	input.TCP.MSSBytes = pointer(model.MaxMSSBytes)
	if _, err := scenario.Normalize(input); err == nil {
		t.Fatal("large MSS accepted a slow-start threshold below two MSS")
	}
	input.TCP.CongestionControl.InitialSlowStartThresholdBytes = pointer(uint64(2 * model.MaxMSSBytes))
	if _, err := scenario.Normalize(input); err != nil {
		t.Fatal(err)
	}

	// A count that fits alone can overflow once converted into MSS-sized bytes.
	input.TCP.CongestionControl.InitialWindowSegments = pointer(math.MaxInt64/model.MaxMSSBytes + 1)
	if _, err := scenario.Normalize(input); err == nil {
		t.Fatal("window count accepted despite overflowing its MSS byte conversion")
	}

	// Tiny synthetic MSS is also valid; its two-MSS threshold is only two bytes.
	input.TCP.MSSBytes = pointer(1)
	input.TCP.CongestionControl.InitialWindowSegments = pointer(4)
	input.TCP.CongestionControl.InitialSlowStartThresholdBytes = pointer(uint64(2))
	if _, err := scenario.Normalize(input); err != nil {
		t.Fatal(err)
	}
}
