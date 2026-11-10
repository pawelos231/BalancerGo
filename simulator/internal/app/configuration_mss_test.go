package app

import (
	"testing"
)

// CLI settings reach the scenario TCP profile without a form overlay.
func TestManualMSSConfigurationRoundTrip(t *testing.T) {
	defaults, err := readTestConfiguration(t)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.effectiveMSSBytes() != 1024 {
		t.Fatal("the default MSS changed")
	}

	configured, err := readTestConfiguration(t, "-tcp-mss", "1460")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := configured.prepareScenario()
	if err != nil {
		t.Fatal(err)
	}
	if prepared.plan.Fixed().TCP.MSSBytes != 1460 {
		t.Fatal("MSS was lost between CLI and scenario")
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
}
