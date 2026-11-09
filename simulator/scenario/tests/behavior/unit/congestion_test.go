package scenario_test

import (
	"math"
	"strings"
	"testing"

	"load-balancer/model"
	"load-balancer/scenario"
)

func TestCongestionDefaultsAndDocumentRoundTrip(t *testing.T) {
	// Older scenarios start with Reno and four 1,024-byte segments in flight.
	input := exampleScenario()
	resolved, err := scenario.NormalizeV2(input)
	if err != nil {
		t.Fatal(err)
	}
	defaults := model.DefaultCongestionConfig()
	congestion := resolved.Fixed.TCP.CongestionControl
	if congestion.Mode != string(defaults.Mode) ||
		congestion.FastRetransmit != defaults.FastRetransmit ||
		congestion.CubicFastConvergence != defaults.CubicFastConvergence ||
		congestion.InitialWindowSegments != defaults.InitialWindowSegments ||
		congestion.InitialSlowStartThresholdBytes != defaults.InitialSlowStartThresholdBytes {
		t.Fatalf("unexpected congestion defaults: %+v", congestion)
	}
	if input.Fixed.TCP.CongestionControl.Mode != nil {
		t.Fatal("normalization added defaults to the original document")
	}

	// Disabling the sender limit must survive export, as must a large exact threshold.
	input.Fixed.TCP.CongestionControl = scenario.CongestionConfig{
		Mode:                           pointer("none"),
		InitialWindowSegments:          pointer(2),
		InitialSlowStartThresholdBytes: pointer(uint64(9_007_199_254_740_993)),
	}
	document, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), `"initialSlowStartThresholdBytes":"9007199254740993"`) {
		t.Fatal("export rounded a threshold larger than JavaScript's safe integer range")
	}
	restored, err := scenario.DecodeDocumentV2(document)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = scenario.NormalizeV2(restored)
	if err != nil {
		t.Fatal(err)
	}
	congestion = resolved.Fixed.TCP.CongestionControl
	if congestion.Mode != "none" || congestion.InitialWindowSegments != 2 ||
		congestion.InitialSlowStartThresholdBytes != 9_007_199_254_740_993 {
		t.Fatalf("congestion settings changed during import/export: %+v", congestion)
	}
}

// Omitted recovery follows the mode; exporting an explicit false keeps it disabled.
func TestFastRetransmitDefaultsAndDocumentRoundTrip(t *testing.T) {
	input := exampleScenario()
	resolved, err := scenario.NormalizeV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Fixed.TCP.CongestionControl.FastRetransmit || input.Fixed.TCP.CongestionControl.FastRetransmit != nil {
		t.Fatal("Reno should inherit fast recovery without mutating the input")
	}

	input.Fixed.TCP.CongestionControl.FastRetransmit = pointer(false)
	document, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), `"fastRetransmit":false`) {
		t.Fatal("export omitted the explicit false")
	}
	restored, err := scenario.DecodeDocumentV2(document)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = scenario.NormalizeV2(restored)
	if err != nil || resolved.Fixed.TCP.CongestionControl.FastRetransmit {
		t.Fatalf("disabled recovery changed after export/import: %+v, %v", resolved.Fixed.TCP, err)
	}

	// The rwnd-only mode omits recovery by default and rejects an incompatible true.
	input.Fixed.TCP.CongestionControl = scenario.CongestionConfig{Mode: pointer("none")}
	resolved, err = scenario.NormalizeV2(input)
	if err != nil || resolved.Fixed.TCP.CongestionControl.FastRetransmit {
		t.Fatalf("none should inherit disabled recovery: %+v, %v", resolved.Fixed.TCP, err)
	}
	input.Fixed.TCP.CongestionControl.FastRetransmit = pointer(true)
	_, err = scenario.NormalizeV2(input)
	if err == nil || !strings.Contains(err.Error(), "tcp.congestionControl.fastRetransmit") {
		t.Fatalf("expected a recovery dependency error, got %v", err)
	}
}

// Native JSON keeps mode selection and omitted or explicit retry settings.
func TestRecoveryModeCongestionSettingsRoundTrip(t *testing.T) {
	tests := []struct {
		name           string
		mode           string
		fastRetransmit *bool
		wantEnabled    bool
	}{
		{name: "Tahoe omitted early retry", mode: "tahoe", wantEnabled: true},
		{name: "Tahoe enabled", mode: "tahoe", fastRetransmit: pointer(true), wantEnabled: true},
		{name: "Tahoe disabled", mode: "tahoe", fastRetransmit: pointer(false)},
		{name: "NewReno omitted early retry", mode: "newReno", wantEnabled: true},
		{name: "NewReno enabled", mode: "newReno", fastRetransmit: pointer(true), wantEnabled: true},
		{name: "NewReno disabled", mode: "newReno", fastRetransmit: pointer(false)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			input.Fixed.TCP.CongestionControl = scenario.CongestionConfig{
				Mode:           pointer(test.mode),
				FastRetransmit: test.fastRetransmit,
			}

			// Encoding must preserve omission as well as an explicit false.
			document, err := scenario.EncodeDocumentV2(input)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := scenario.DecodeDocumentV2(document)
			if err != nil {
				t.Fatal(err)
			}
			if (restored.Fixed.TCP.CongestionControl.FastRetransmit == nil) != (test.fastRetransmit == nil) {
				t.Fatal("native round trip changed whether early retry was configured")
			}
			resolved, err := scenario.NormalizeV2(restored)
			if err != nil {
				t.Fatal(err)
			}
			config := resolved.Fixed.TCP.CongestionControl
			if config.Mode != test.mode || config.FastRetransmit != test.wantEnabled ||
				config.InitialWindowSegments != 4 || config.InitialSlowStartThresholdBytes != 65535 {
				t.Fatalf("resolved %s configuration = %+v", test.mode, config)
			}
		})
	}
}

func TestCongestionValidation(t *testing.T) {
	// Zero is invalid even in mode none, so enabling Reno later cannot reveal bad settings.
	for _, mode := range []string{"none", "reno", "newReno", "tahoe", "cubic", "", "bbr"} {
		input := exampleScenario()
		input.Fixed.TCP.CongestionControl.Mode = pointer(mode)
		_, err := scenario.NormalizeV2(input)
		valid := mode == "none" || mode == "reno" || mode == "newReno" ||
			mode == "tahoe" || mode == "cubic"
		if (err == nil) != valid {
			t.Fatalf("mode %q: error %v", mode, err)
		}
	}

	// The largest segment count can still be multiplied by the 1,024-byte MSS.
	maximumSegments := math.MaxInt64 / model.SEGMENT_SIZE
	for _, segments := range []int{-1, 0, 1, maximumSegments, maximumSegments + 1} {
		input := exampleScenario()
		input.Fixed.TCP.CongestionControl.InitialWindowSegments = pointer(segments)
		_, err := scenario.NormalizeV2(input)
		valid := segments >= 1 && segments <= maximumSegments
		if (err == nil) != valid {
			t.Fatalf("initial window %d: error %v", segments, err)
		}
	}

	// The threshold holds at least two segments and fits the sender's int64 byte range.
	for _, threshold := range []uint64{0, 2047, 2048, math.MaxInt64, math.MaxInt64 + 1} {
		input := exampleScenario()
		input.Fixed.TCP.CongestionControl.InitialSlowStartThresholdBytes = pointer(threshold)
		_, err := scenario.NormalizeV2(input)
		valid := threshold >= 2048 && threshold <= math.MaxInt64
		if (err == nil) != valid {
			t.Fatalf("initial threshold %d: error %v", threshold, err)
		}
	}
}
