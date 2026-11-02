package main

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/model"
	"load-balancer/scenario"
)

func TestCloseConfigurationRoundTripAndOmission(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	if requested.TCPMSLMs != 30000 || requested.TCPFinWaitTimeoutMs != 60000 || requested.TCPMaxFINRetries != 5 {
		t.Fatal("legacy configuration lost shared close defaults")
	}
	requested.Bytes = 64
	requested.TCPMSLMs = 12.5
	requested.TCPFinWaitTimeoutMs = 750
	requested.TCPMaxFINRetries = 0
	runtime, err := configurationFromPublic(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.publicConfig() != requested || defaults.closeConfig != nil {
		t.Fatal("legacy overlay changed values or modified its defaults")
	}
	resolved, err := scenario.Normalize(runtime.scenarioDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TCP.Close.MSL != 12500*time.Microsecond || resolved.TCP.Close.FinWaitTimeout != 750*time.Millisecond ||
		resolved.TCP.Close.MaxFINRetries != 0 {
		t.Fatalf("native mapping lost close settings: %+v", resolved.TCP.Close)
	}
	summary := scenarioSummary(resolved)
	if summary.TCPMSLMs != requested.TCPMSLMs || summary.TCPFinWaitTimeoutMs != requested.TCPFinWaitTimeoutMs ||
		summary.TCPMaxFINRetries != 0 {
		t.Fatal("scenario summary lost close settings")
	}

	// Defaults supplied by the process survive an older WebSocket payload.
	policy := model.CloseConfig{MSL: time.Second, FinWaitTimeout: 2 * time.Second, MaxFINRetries: 3}
	defaults.closeConfig = &policy
	prepared, err := prepareStart(startRequest{legacy: &runtimeCommand{kind: startCommand, Payload: json.RawMessage(`{"bytes":0}`)}}, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.scenario.TCP.Close.MSL != policy.MSL || prepared.scenario.TCP.Close.MaxFINRetries != 3 {
		t.Fatal("omitted legacy close settings lost process defaults")
	}
	prepared, err = prepareStart(startRequest{legacy: &runtimeCommand{kind: startCommand, Payload: json.RawMessage(`{"tcpMaxFINRetries":0}`)}}, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.scenario.TCP.Close.MaxFINRetries != 0 || policy.MaxFINRetries != 3 {
		t.Fatal("explicit zero FIN retries was replaced or mutated the defaults")
	}
}

func TestCloseCLIFlagsAndInvalidPublicValues(t *testing.T) {
	cfg, err := readTestConfiguration(t, "-tcp-msl", "10ms", "-tcp-fin-wait-timeout", "2s", "-tcp-fin-retries", "0")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.effectiveCloseConfig(); got.MSL != 10*time.Millisecond || got.FinWaitTimeout != 2*time.Second || got.MaxFINRetries != 0 {
		t.Fatalf("CLI lost close settings: %+v", got)
	}
	for _, arguments := range [][]string{
		{"-tcp-msl", "0"}, {"-tcp-msl", "-1s"}, {"-tcp-msl", "1281024h"},
		{"-tcp-fin-wait-timeout", "0"}, {"-tcp-fin-retries", "-1"}, {"-tcp-fin-retries", "31"},
	} {
		if _, err := readTestConfiguration(t, arguments...); err == nil {
			t.Fatalf("invalid close flags accepted: %v", arguments)
		}
	}
	for _, change := range []func(*contract.SimulationConfig){
		func(c *contract.SimulationConfig) { c.TCPMSLMs = 0 },
		func(c *contract.SimulationConfig) { c.TCPMSLMs = math.Inf(1) },
		func(c *contract.SimulationConfig) { c.TCPMSLMs = float64(math.MaxInt64) / float64(time.Millisecond) },
		func(c *contract.SimulationConfig) { c.TCPFinWaitTimeoutMs = math.NaN() },
		func(c *contract.SimulationConfig) { c.TCPMaxFINRetries = 31 },
	} {
		requested := legacyTestConfiguration().publicConfig()
		change(&requested)
		if _, err := configurationFromPublic(requested, legacyTestConfiguration()); err == nil {
			t.Fatalf("invalid public close configuration accepted: %+v", requested)
		}
	}
}
