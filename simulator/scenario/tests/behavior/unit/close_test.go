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
	resolved, err := scenario.NormalizeV2(input)
	if err != nil {
		t.Fatal(err)
	}
	defaults := model.DefaultCloseConfig()
	if resolved.Fixed.TCP.Close.MSL != defaults.MSL || resolved.Fixed.TCP.Close.FinWaitTimeout != defaults.FinWaitTimeout ||
		resolved.Fixed.TCP.Close.MaxFINRetries != defaults.MaxFINRetries {
		t.Fatalf("older scenario lost close defaults: %+v", resolved.Fixed.TCP.Close)
	}
	if input.Fixed.TCP.Close.MSL != nil || input.Fixed.TCP.Close.MaxFINRetries != nil {
		t.Fatal("normalization modified the input")
	}

	input.Fixed.TCP.Close = scenario.CloseConfig{
		MSL:            pointer(time.Duration(9_007_199_254_740_993)),
		FinWaitTimeout: pointer(2 * time.Second), MaxFINRetries: pointer(0),
	}
	document, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document, []byte(`"mslNanoseconds":"9007199254740993"`)) ||
		!bytes.Contains(document, []byte(`"maxFINRetries":0`)) {
		t.Fatalf("document lost exact duration or zero retries: %s", document)
	}
	restored, err := scenario.DecodeDocumentV2(document)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = scenario.NormalizeV2(restored)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fixed.TCP.Close.MSL != *input.Fixed.TCP.Close.MSL || resolved.Fixed.TCP.Close.FinWaitTimeout != 2*time.Second ||
		resolved.Fixed.TCP.Close.MaxFINRetries != 0 {
		t.Fatalf("close settings changed on import: %+v", resolved.Fixed.TCP.Close)
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
			input.Fixed.TCP.Close = test.config
			if _, err := scenario.NormalizeV2(input); err == nil || !strings.Contains(err.Error(), "tcp.close."+test.path) {
				t.Fatalf("wanted close validation error, got %v", err)
			}
		})
	}
	input := exampleScenario()
	input.Fixed.TCP.Close = scenario.CloseConfig{
		MSL:            pointer(time.Duration(math.MaxInt64 / 2)),
		FinWaitTimeout: pointer(time.Duration(math.MaxInt64)), MaxFINRetries: pointer(30),
	}
	if _, err := scenario.NormalizeV2(input); err != nil {
		t.Fatalf("valid timer boundaries rejected: %v", err)
	}
}
