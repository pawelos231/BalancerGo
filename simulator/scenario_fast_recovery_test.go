package main

import (
	"load-balancer/scenario"
	"testing"
)

// Native WS Start keeps omitted defaults and explicit false in the TCP document.
func TestNativeStartResolvesRecoverySettings(t *testing.T) {
	for _, mode := range []string{"none", "reno", "newReno", "tahoe", "cubic"} {
		t.Run(mode, func(t *testing.T) {
			for _, explicitDisabled := range []bool{false, true} {
				input := testConfiguration().scenarioDefinitionV2()
				input.Fixed.TCP.CongestionControl = scenario.CongestionConfig{Mode: &mode}
				if explicitDisabled {
					input.Fixed.TCP.CongestionControl.FastRetransmit = scenarioRuntimeValue(false)
				}
				document, err := scenario.EncodeDocumentV2(input)
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := prepareStart(startRequest{command: &runtimeCommand{Payload: document}}, testConfiguration())
				if err != nil {
					t.Fatal(err)
				}

				wantEnabled := mode != "none" && !explicitDisabled
				actual := prepared.plan.Fixed().TCP.CongestionControl
				if actual.FastRetransmit != wantEnabled || prepared.config.FastRetransmit != wantEnabled {
					t.Fatalf("recovery = %+v, want enabled=%v", actual, wantEnabled)
				}
			}
		})
	}
}
