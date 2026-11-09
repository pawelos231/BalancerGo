package scenario_test

import (
	"testing"

	"load-balancer/scenario"
)

// Omitted convergence uses the CUBIC default; an explicit false survives scenario JSON.
func TestCubicOptionsSurviveDocumentRoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		fastConvergence *bool
		fastRetransmit  *bool
		wantConvergence bool
		wantRetransmit  bool
	}{
		{name: "shared defaults", wantConvergence: true, wantRetransmit: true},
		{
			name: "single flow disables convergence", fastConvergence: pointer(false),
			wantConvergence: false, wantRetransmit: true,
		},
		{
			name: "timeout only", fastRetransmit: pointer(false),
			wantConvergence: true, wantRetransmit: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			input.Fixed.TCP.CongestionControl = scenario.CongestionConfig{
				Mode:                 pointer("cubic"),
				CubicFastConvergence: test.fastConvergence,
				FastRetransmit:       test.fastRetransmit,
			}
			document, err := scenario.EncodeDocument(input)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := scenario.DecodeDocument(document)
			if err != nil {
				t.Fatal(err)
			}

			// Omission and false remain distinct until normalization fills defaults.
			if (restored.Fixed.TCP.CongestionControl.CubicFastConvergence == nil) !=
				(test.fastConvergence == nil) {
				t.Fatal("scenario JSON changed whether fast convergence was configured")
			}
			resolved, err := scenario.Normalize(restored)
			if err != nil {
				t.Fatal(err)
			}
			config := resolved.Fixed.TCP.CongestionControl
			if config.Mode != "cubic" || config.CubicFastConvergence != test.wantConvergence ||
				config.FastRetransmit != test.wantRetransmit {
				t.Fatalf("resolved CUBIC settings = %+v", config)
			}
		})
	}
}
