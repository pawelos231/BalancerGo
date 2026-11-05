package main

import "testing"

// CLI, the browser adapter and the native scenario keep the same manual payload cap.
func TestManualMSSConfigurationRoundTrip(t *testing.T) {
	defaults, err := readTestConfiguration(t)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.publicConfig().MSSBytes != 1024 {
		t.Fatal("the default MSS changed")
	}

	configured, err := readTestConfiguration(t, "-tcp-mss", "1460")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := preparePublicScenario(configured.publicConfig(), defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.resolved.TCP.MSSBytes != 1460 || scenarioSummary(prepared.resolved).MSSBytes != 1460 {
		t.Fatal("MSS was lost between CLI, scenario and browser summary")
	}

	// Explicit zero must not become the legacy internal fallback of 1,024 bytes.
	for _, flags := range [][]string{
		{"-tcp-mss", "0"},
		{"-tcp-mss", "-1"},
		{"-tcp-mss", "65496"},
	} {
		if _, err := readTestConfiguration(t, flags...); err == nil {
			t.Fatalf("invalid MSS accepted: %v", flags)
		}
	}
	invalid := defaults.publicConfig()
	invalid.MSSBytes = 0
	if _, err := preparePublicScenario(invalid, defaults); err == nil {
		t.Fatal("explicit zero MSS accepted from the browser")
	}
}
