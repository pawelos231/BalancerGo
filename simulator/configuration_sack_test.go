package main

import (
	"testing"
)

// The old form and native summary carry both explicit SACK choices unchanged.
func TestConfigurationSACKRoundTrip(t *testing.T) {
	defaults := legacyTestConfiguration()
	if defaults.publicConfig().SACKEnabled {
		t.Fatal("SACK should be opt-in for existing configurations")
	}

	for _, enabled := range []bool{false, true} {
		requested := defaults.publicConfig()
		requested.SACKEnabled = enabled

		prepared, err := preparePublicScenario(requested, defaults)
		if err != nil {
			t.Fatal(err)
		}
		resolved := prepared.resolved

		if *prepared.definition.TCP.SACKEnabled != enabled ||
			resolved.TCP.SACKEnabled != enabled || scenarioSummary(resolved).SACKEnabled != enabled {
			t.Fatalf("SACK enabled=%v was lost by a configuration adapter", enabled)
		}
	}
}
