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
		{name: "NewReno default", payload: `{"congestionControl":"newReno"}`, want: true},
		{
			name:    "NewReno explicit disabled",
			payload: `{"congestionControl":"newReno","fastRetransmit":false}`,
		},
		{name: "Tahoe default", payload: `{"congestionControl":"tahoe"}`, want: true},
		{name: "CUBIC default", payload: `{"congestionControl":"cubic"}`, want: true},
		{
			name:    "CUBIC explicit disabled",
			payload: `{"congestionControl":"cubic","fastRetransmit":false}`,
		},
		{
			name:    "Tahoe explicit disabled",
			payload: `{"congestionControl":"tahoe","fastRetransmit":false}`,
		},
		{name: "explicit disabled", payload: `{"fastRetransmit":false}`},
		{
			name:     "none to NewReno defaults enabled",
			payload:  `{"congestionControl":"newReno"}`,
			baseMode: model.CongestionNone,
			want:     true,
		},
		{
			name:     "none to Tahoe defaults enabled",
			payload:  `{"congestionControl":"tahoe"}`,
			baseMode: model.CongestionNone,
			want:     true,
		},
		{
			name:     "none to CUBIC defaults enabled",
			payload:  `{"congestionControl":"cubic"}`,
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

// Legacy WS input keeps an explicit false instead of replacing it with the CUBIC default.
func TestLegacyStartPreservesCubicFastConvergence(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{
			name:    "omitted default",
			payload: `{"congestionControl":"cubic"}`,
			want:    true,
		},
		{
			name:    "explicit disabled",
			payload: `{"congestionControl":"cubic","cubicFastConvergence":false}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := startRequest{
				legacy: &runtimeCommand{Payload: json.RawMessage(test.payload)},
			}
			prepared, err := prepareStart(request, legacyTestConfiguration())
			if err != nil {
				t.Fatal(err)
			}
			actual := prepared.scenario.TCP.CongestionControl
			if actual.Mode != "cubic" || actual.CubicFastConvergence != test.want ||
				prepared.config.CubicFastConvergence != test.want {
				t.Fatalf("resolved CUBIC configuration = %+v", actual)
			}
		})
	}
}
