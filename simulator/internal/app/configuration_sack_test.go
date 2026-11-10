package app

import "testing"

func TestSACKCLIProfileReachesNativeTCP(t *testing.T) {
	for _, mode := range []string{"reno", "newReno", "cubic"} {
		t.Run(mode, func(t *testing.T) {
			configured, err := readTestConfiguration(t, "-sack-enabled", "-tcp-congestion-control", mode)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := configured.prepareScenario()
			if err != nil {
				t.Fatal(err)
			}
			if !prepared.plan.Fixed().TCP.SACKEnabled {
				t.Fatal("scenario TCP lost the SACK flag")
			}
		})
	}
}

// The CLI settings and public summary carry both explicit SACK choices unchanged.
