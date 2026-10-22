package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"load-balancer/model"
)

func legacyTestConfiguration() configuration {
	return configuration{
		clients:                 2,
		servers:                 2,
		packets:                 3,
		streamBytes:             -1,
		duration:                time.Second,
		snapshotInterval:        100 * time.Millisecond,
		packetTravelTime:        time.Millisecond,
		bandwidth:               1_000_000_000,
		forwardingBitsPerSecond: 1_000_000_000,
		queue:                   1024,
		seed:                    1,
		algorithm:               "roundRobin",
		maxEvents:               100_000,
	}
}

func TestConfigurationKeepsForwardingAndLinkBandwidthIndependent(t *testing.T) {
	cfg := legacyTestConfiguration()
	cfg.bandwidth = 125_000_000
	cfg.forwardingBitsPerSecond = 750_000_000

	runtime, err := cfg.normalized()
	if err != nil {
		t.Fatal(err)
	}
	definition := runtime.scenarioDefinition()
	if definition.Network.Defaults.BandwidthBitsPerSecond != cfg.bandwidth {
		t.Fatal("scenario link bandwidth changed")
	}
	if definition.Resources.LoadBalancer.ForwardingBitsPerSecond != cfg.forwardingBitsPerSecond {
		t.Fatal("scenario forwarding budget inherited link bandwidth")
	}

	// The browser-facing values must survive the full configuration round trip.
	requested := cfg.publicConfig()
	if requested.BandwidthBitsPerSecond != cfg.bandwidth {
		t.Fatal("public link bandwidth changed")
	}
	if requested.ForwardingBitsPerSecond != cfg.forwardingBitsPerSecond {
		t.Fatal("public forwarding budget changed")
	}
	restored, err := requested.configuration(legacyTestConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if restored.bandwidth != cfg.bandwidth || restored.forwardingBitsPerSecond != cfg.forwardingBitsPerSecond {
		t.Fatal("configuration overlay mixed the two bandwidth limits")
	}
}

func TestConfigurationOmittedForwardingBudgetUsesDefaults(t *testing.T) {
	defaults := legacyTestConfiguration()
	defaults.forwardingBitsPerSecond = 600_000_000
	requested := defaults.publicConfig()

	// A partial WebSocket payload changes the link rate, not the omitted LB rate.
	input := `{"bandwidthBitsPerSecond":250000000}`
	if err := json.Unmarshal([]byte(input), &requested); err != nil {
		t.Fatal(err)
	}
	runtime, err := requested.configuration(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.bandwidth != 250_000_000 {
		t.Fatal("explicit link bandwidth override ignored")
	}
	if runtime.forwardingBitsPerSecond != defaults.forwardingBitsPerSecond {
		t.Fatal("omitted forwarding budget did not inherit the Go default")
	}
	if defaults.bandwidth != 1_000_000_000 || defaults.forwardingBitsPerSecond != 600_000_000 {
		t.Fatal("normalization mutated the original defaults")
	}
}

func TestConfigurationForwardingOverrideKeepsLinkDefault(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	input := `{"forwardingBitsPerSecond":250000000}`
	if err := json.Unmarshal([]byte(input), &requested); err != nil {
		t.Fatal(err)
	}

	runtime, err := requested.configuration(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.forwardingBitsPerSecond != 250_000_000 {
		t.Fatal("explicit forwarding budget override ignored")
	}
	if runtime.bandwidth != defaults.bandwidth {
		t.Fatal("forwarding budget override changed the link bandwidth")
	}
}

func TestConfigurationRejectsNonpositiveForwardingBudget(t *testing.T) {
	tests := []struct {
		name   string
		budget int64
		input  string
	}{
		{
			name:   "zero",
			budget: 0,
			input:  `{"forwardingBitsPerSecond":0}`,
		},
		{
			name:   "negative",
			budget: -1,
			input:  `{"forwardingBitsPerSecond":-1}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Native configuration and explicit JSON overrides use the same validator.
			cfg := legacyTestConfiguration()
			cfg.forwardingBitsPerSecond = test.budget
			if _, err := cfg.normalized(); err == nil {
				t.Fatal("nonpositive native forwarding budget accepted")
			}

			defaults := legacyTestConfiguration()
			requested := defaults.publicConfig()
			if err := json.Unmarshal([]byte(test.input), &requested); err != nil {
				t.Fatal(err)
			}
			_, err := requested.configuration(defaults)
			if err == nil || !strings.Contains(err.Error(), "forwardingBitsPerSecond") {
				t.Fatalf("expected forwarding budget validation error, got %v", err)
			}
		})
	}
}

func TestConfigurationKeepsRawStreamDefaults(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	if err := json.Unmarshal([]byte(`{"packets":5}`), &requested); err != nil {
		t.Fatal(err)
	}

	runtime, err := requested.configuration(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.streamBytes != 5*model.SEGMENT_SIZE {
		t.Fatalf("payload bytes = %d", runtime.streamBytes)
	}
	if defaults.streamBytes != -1 || requested.Bytes != -1 {
		t.Fatal("normalization changed the unresolved UI/default configuration")
	}

	requested.Bytes = 17
	requested.Packets = 8
	runtime, err = requested.configuration(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.streamBytes != 17 {
		t.Fatalf("explicit payload bytes = %d", runtime.streamBytes)
	}
}

func TestScenarioAdapterPreservesExactDurations(t *testing.T) {
	cfg := legacyTestConfiguration()
	cfg.duration = 15 * time.Nanosecond
	cfg.snapshotInterval = 7 * time.Nanosecond
	cfg.clientGap = 3 * time.Nanosecond
	cfg.packetGap = 5 * time.Nanosecond
	cfg.packetTravelTime = 11 * time.Nanosecond

	runtime, err := cfg.normalized()
	if err != nil {
		t.Fatal(err)
	}
	definition := runtime.scenarioDefinition()
	if definition.Execution.Duration != cfg.duration {
		t.Fatal("experiment duration lost nanosecond precision")
	}
	if definition.Execution.SnapshotInterval != cfg.snapshotInterval {
		t.Fatal("snapshot interval changed")
	}
	if definition.Workload.ClientGroups[0].ClientGap != cfg.clientGap {
		t.Fatal("client gap changed")
	}
	if definition.Workload.Defaults.PacketGap != cfg.packetGap {
		t.Fatal("packet gap changed")
	}
	if definition.Network.Defaults.PacketTravelTime != cfg.packetTravelTime {
		t.Fatal("packet travel time changed")
	}
}

func TestConfigurationAcceptsExplicitZeros(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	input := `{"clients":0,"servers":0,"bytes":0,"durationSeconds":0,"linkQueue":0,"packetTravelTimeMs":0,"speed":0}`
	if err := json.Unmarshal([]byte(input), &requested); err != nil {
		t.Fatal(err)
	}

	runtime, err := requested.configuration(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.clients != 0 || runtime.servers != 0 || runtime.streamBytes != 0 {
		t.Fatal("zero counts or stream size inherited a default")
	}
	if runtime.duration != 0 || runtime.queue != 0 || runtime.packetTravelTime != 0 || runtime.speed != 0 {
		t.Fatal("zero resource settings inherited a default")
	}
}

func TestHeadlessConfigurationValidatesSpeedBeforeDisablingPacing(t *testing.T) {
	for _, speed := range []float64{-1, math.NaN(), math.Inf(1)} {
		cfg := legacyTestConfiguration()
		cfg.headless = true
		cfg.speed = speed
		if _, err := cfg.normalized(); err == nil {
			t.Errorf("invalid headless speed accepted: %v", speed)
		}
	}

	cfg := legacyTestConfiguration()
	cfg.headless = true
	cfg.tracePath = "example.trace"
	cfg.duration = time.Duration(math.MaxInt64)
	cfg.speed = 1e-100
	runtime, err := cfg.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.speed != 0 || !runtime.headless || runtime.tracePath != cfg.tracePath {
		t.Fatal("normalization lost process options or left headless pacing enabled")
	}

	cfg.headless = false
	if _, err := cfg.normalized(); err == nil {
		t.Fatal("paced duration overflow accepted")
	}
}

func TestConfigurationPreservesLegacyIntegerLimits(t *testing.T) {
	cfg := legacyTestConfiguration()
	cfg.clients = (1 << 24) - 1
	cfg.servers = 0
	cfg.seed = math.MinInt64
	cfg.maxEvents = math.MaxUint64
	cfg.bandwidth = math.MaxInt64
	cfg.forwardingBitsPerSecond = math.MaxInt64
	if _, err := cfg.normalized(); err != nil {
		t.Fatal(err)
	}

	cfg.clients++
	if _, err := cfg.normalized(); err == nil {
		t.Fatal("client address range overflow accepted")
	}
	cfg.clients = 0
	cfg.servers = 10001
	if _, err := cfg.normalized(); err == nil {
		t.Fatal("backend limit exceeded")
	}
}

func TestConfigurationKeepsSimplifiedTransportLimits(t *testing.T) {
	if math.MaxInt < math.MaxInt64 {
		t.Skip("large stream boundary requires a 64-bit int")
	}
	cfg := legacyTestConfiguration()
	maxSegmentCount := uint64(math.MaxUint32)
	firstInvalidBytes := int(maxSegmentCount * uint64(model.SEGMENT_SIZE))
	cfg.streamBytes = firstInvalidBytes - 1
	if _, err := cfg.normalized(); err != nil {
		t.Fatal(err)
	}
	cfg.streamBytes++
	if _, err := cfg.normalized(); err == nil {
		t.Fatal("segment counter overflow accepted")
	}
	cfg.streamBytes = 17
	cfg.packets = -1
	if _, err := cfg.normalized(); err == nil {
		t.Fatal("negative packet count accepted with explicit payload size")
	}
}
