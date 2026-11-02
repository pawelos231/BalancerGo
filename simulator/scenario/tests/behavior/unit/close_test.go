package scenario_test

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

// Older documents use the shared limits, while zero explicitly disables FIN retries.
func TestCloseDefaultsAndExactDocumentRoundTrip(t *testing.T) {
	input := exampleScenario()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	defaults := model.DefaultCloseConfig()
	if resolved.TCP.Close.MSL != defaults.MSL || resolved.TCP.Close.FinWaitTimeout != defaults.FinWaitTimeout ||
		resolved.TCP.Close.MaxFINRetries != defaults.MaxFINRetries {
		t.Fatalf("older scenario lost close defaults: %+v", resolved.TCP.Close)
	}
	if input.TCP.Close.MSL != nil || input.TCP.Close.MaxFINRetries != nil {
		t.Fatal("normalization modified the input")
	}

	input.TCP.Close = scenario.CloseConfig{
		MSL:            pointer(time.Duration(9_007_199_254_740_993)),
		FinWaitTimeout: pointer(2 * time.Second), MaxFINRetries: pointer(0),
	}
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document, []byte(`"mslNanoseconds":"9007199254740993"`)) ||
		!bytes.Contains(document, []byte(`"maxFINRetries":0`)) {
		t.Fatalf("document lost exact duration or zero retries: %s", document)
	}
	restored, err := scenario.DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = scenario.Normalize(restored)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TCP.Close.MSL != *input.TCP.Close.MSL || resolved.TCP.Close.FinWaitTimeout != 2*time.Second ||
		resolved.TCP.Close.MaxFINRetries != 0 {
		t.Fatalf("close settings changed on import: %+v", resolved.TCP.Close)
	}
}

func TestCloseLimits(t *testing.T) {
	for _, test := range []struct {
		name   string
		config scenario.CloseConfig
		path   string
	}{
		{"zero MSL", scenario.CloseConfig{MSL: pointer(time.Duration(0))}, "mslNanoseconds"},
		{"negative MSL", scenario.CloseConfig{MSL: pointer(-time.Second)}, "mslNanoseconds"},
		{"overflow twice MSL", scenario.CloseConfig{MSL: pointer(time.Duration(math.MaxInt64/2 + 1))}, "mslNanoseconds"},
		{"zero wait", scenario.CloseConfig{FinWaitTimeout: pointer(time.Duration(0))}, "finWaitTimeoutNanoseconds"},
		{"negative wait", scenario.CloseConfig{FinWaitTimeout: pointer(-time.Second)}, "finWaitTimeoutNanoseconds"},
		{"negative retries", scenario.CloseConfig{MaxFINRetries: pointer(-1)}, "maxFINRetries"},
		{"too many retries", scenario.CloseConfig{MaxFINRetries: pointer(31)}, "maxFINRetries"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			input.TCP.Close = test.config
			if _, err := scenario.Normalize(input); err == nil || !strings.Contains(err.Error(), "tcp.close."+test.path) {
				t.Fatalf("wanted close validation error, got %v", err)
			}
		})
	}
	input := exampleScenario()
	input.TCP.Close = scenario.CloseConfig{
		MSL:            pointer(time.Duration(math.MaxInt64 / 2)),
		FinWaitTimeout: pointer(time.Duration(math.MaxInt64)), MaxFINRetries: pointer(30),
	}
	if _, err := scenario.Normalize(input); err != nil {
		t.Fatalf("valid timer boundaries rejected: %v", err)
	}
}
