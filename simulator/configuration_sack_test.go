package main

import "testing"

func TestSACKCLIProfileReachesNativeTCP(t *testing.T) {
	for _, mode := range []string{"reno", "newReno", "cubic"} {
		t.Run(mode, func(t *testing.T) {
			configured, err := readTestConfiguration(t, "-sack-enabled", "-tcp-congestion-control", mode)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := configured.prepareScenarioV2()
			if err != nil {
				t.Fatal(err)
			}
			if !prepared.plan.Fixed().TCP.SACKEnabled {
				t.Fatal("native TCP lost the SACK flag")
			}
		})
	}
}

// The old form and native summary carry both explicit SACK choices unchanged.
