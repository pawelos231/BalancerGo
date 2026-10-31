package scenario

import (
	"math"
	"strings"
	"testing"

	"load-balancer/model"
)

func TestCongestionDefaultsAndDocumentRoundTrip(t *testing.T) {
	// Older scenarios start with Reno and four 1,024-byte segments in flight.
	input := exampleScenario()
	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	defaults := model.DefaultCongestionConfig()
	congestion := resolved.TCP.CongestionControl
	if congestion.Mode != string(defaults.Mode) ||
		congestion.InitialWindowSegments != defaults.InitialWindowSegments ||
		congestion.InitialSlowStartThresholdBytes != defaults.InitialSlowStartThresholdBytes {
		t.Fatalf("unexpected congestion defaults: %+v", congestion)
	}
	if input.TCP.CongestionControl.Mode != nil {
		t.Fatal("normalization added defaults to the original document")
	}

	// Disabling the sender limit must survive export, as must a large exact threshold.
	input.TCP.CongestionControl = CongestionConfig{
		Mode:                           pointer("none"),
		InitialWindowSegments:          pointer(2),
		InitialSlowStartThresholdBytes: pointer(uint64(9_007_199_254_740_993)),
	}
	document, err := EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), `"initialSlowStartThresholdBytes":"9007199254740993"`) {
		t.Fatal("export rounded a threshold larger than JavaScript's safe integer range")
	}
	restored, err := DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = Normalize(restored)
	if err != nil {
		t.Fatal(err)
	}
	congestion = resolved.TCP.CongestionControl
	if congestion.Mode != "none" || congestion.InitialWindowSegments != 2 ||
		congestion.InitialSlowStartThresholdBytes != 9_007_199_254_740_993 {
		t.Fatalf("congestion settings changed during import/export: %+v", congestion)
	}
}

func TestCongestionValidation(t *testing.T) {
	// Zero is invalid even in mode none, so enabling Reno later cannot reveal bad settings.
	for _, mode := range []string{"none", "reno", "", "cubic"} {
		input := exampleScenario()
		input.TCP.CongestionControl.Mode = pointer(mode)
		_, err := Normalize(input)
		valid := mode == "none" || mode == "reno"
		if (err == nil) != valid {
			t.Fatalf("mode %q: error %v", mode, err)
		}
	}

	// The largest segment count can still be multiplied by the 1,024-byte MSS.
	maximumSegments := math.MaxInt64 / model.SEGMENT_SIZE
	for _, segments := range []int{-1, 0, 1, maximumSegments, maximumSegments + 1} {
		input := exampleScenario()
		input.TCP.CongestionControl.InitialWindowSegments = pointer(segments)
		_, err := Normalize(input)
		valid := segments >= 1 && segments <= maximumSegments
		if (err == nil) != valid {
			t.Fatalf("initial window %d: error %v", segments, err)
		}
	}

	// The threshold holds at least two segments and fits the sender's int64 byte range.
	for _, threshold := range []uint64{0, 2047, 2048, math.MaxInt64, math.MaxInt64 + 1} {
		input := exampleScenario()
		input.TCP.CongestionControl.InitialSlowStartThresholdBytes = pointer(threshold)
		_, err := Normalize(input)
		valid := threshold >= 2048 && threshold <= math.MaxInt64
		if (err == nil) != valid {
			t.Fatalf("initial threshold %d: error %v", threshold, err)
		}
	}
}
