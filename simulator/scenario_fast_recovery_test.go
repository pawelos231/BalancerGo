package main

import (
	"encoding/json"
	"testing"

	"load-balancer/model"
)

// An older start payload can select mode none without knowing the recovery flag.
// An explicit true still fails, so omission cannot silently disable a requested feature.
func TestLegacyStartResolvesFastRecoveryFromPresentSettings(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		baseMode model.CongestionMode
		want     bool
		invalid  bool
	}{
		{name: "older Reno", payload: `{}`, want: true},
		{name: "older none", payload: `{"congestionControl":"none"}`},
		{name: "Tahoe default", payload: `{"congestionControl":"tahoe"}`, want: true},
		{
			name:    "Tahoe explicit disabled",
			payload: `{"congestionControl":"tahoe","fastRetransmit":false}`,
		},
		{name: "explicit disabled", payload: `{"fastRetransmit":false}`},
		{
			name:     "none to Tahoe defaults enabled",
			payload:  `{"congestionControl":"tahoe"}`,
			baseMode: model.CongestionNone,
			want:     true,
		},
		{
			name:     "none to Reno defaults enabled",
			payload:  `{"congestionControl":"reno"}`,
			baseMode: model.CongestionNone,
			want:     true,
		},
		{
			name:     "unchanged Reno keeps configured false",
			payload:  `{}`,
			baseMode: model.CongestionReno,
		},
		{
			name:    "incompatible explicit enabled",
			payload: `{"congestionControl":"none","fastRetransmit":true}`,
			invalid: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defaults := legacyTestConfiguration()
			if test.baseMode != "" {
				defaults.congestion = model.DefaultCongestionConfig()
				defaults.congestion.Mode = test.baseMode
				defaults.congestion.FastRetransmit = false
			}
			request := startRequest{
				legacy: &runtimeCommand{Payload: json.RawMessage(test.payload)},
			}
			prepared, err := prepareStart(request, defaults)
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
