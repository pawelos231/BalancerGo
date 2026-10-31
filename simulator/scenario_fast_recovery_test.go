package main

import (
	"encoding/json"
	"testing"
)

// An older start payload can select mode none without knowing the recovery flag.
// An explicit true still fails, so omission cannot silently disable a requested feature.
func TestLegacyStartResolvesFastRecoveryFromPresentSettings(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
		invalid bool
	}{
		{name: "older Reno", payload: `{}`, want: true},
		{name: "older none", payload: `{"congestionControl":"none"}`},
		{name: "explicit disabled", payload: `{"fastRetransmit":false}`},
		{
			name:    "incompatible explicit enabled",
			payload: `{"congestionControl":"none","fastRetransmit":true}`,
			invalid: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := startRequest{
				legacy: &runtimeCommand{Payload: json.RawMessage(test.payload)},
			}
			prepared, err := prepareStart(request, legacyTestConfiguration())
			if test.invalid {
				if err == nil {
					t.Fatal("mode none accepted explicitly enabled fast recovery")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if prepared.scenario.TCP.CongestionControl.FastRetransmit != test.want ||
				prepared.config.FastRetransmit != test.want {
				t.Fatalf("resolved recovery = %+v, want enabled=%v", prepared.scenario.TCP, test.want)
			}
		})
	}
}
