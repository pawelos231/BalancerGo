package main

import (
	"encoding/json"
	"flag"
	"load-balancer/internal/contract"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
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
		ingress:                 scenario.DefaultIngressConfig(),
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
	restored, err := configurationFromPublic(requested, legacyTestConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if restored.bandwidth != cfg.bandwidth || restored.forwardingBitsPerSecond != cfg.forwardingBitsPerSecond {
		t.Fatal("configuration overlay mixed the two bandwidth limits")
	}
}

func TestConfigurationTCPReceiveBufferRoundTrip(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	if requested.ReceiveBufferBytes != model.DefaultReceiveBufferBytes {
		t.Fatal("legacy defaults omitted the TCP receive-buffer capacity")
	}

	requested.ReceiveBufferBytes = 64
	runtime, err := configurationFromPublic(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	definition := runtime.scenarioDefinition()
	if definition.TCP.ReceiveBufferBytes == nil || *definition.TCP.ReceiveBufferBytes != 64 {
		t.Fatal("the form overlay lost the TCP receive-buffer capacity")
	}

	for _, invalid := range []int{0, -1, 65536} {
		requested.ReceiveBufferBytes = invalid
		if _, err := configurationFromPublic(requested, defaults); err == nil {
			t.Fatalf("invalid receive buffer %d was accepted", invalid)
		}
	}
}

func TestTCPReceiveBufferCLIFlag(t *testing.T) {
	cfg, err := readTestConfiguration(t, "-tcp-receive-buffer", "64")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.receiveBufferBytes != 64 {
		t.Fatal("the CLI ignored the receive-buffer setting")
	}

	for _, value := range []string{"0", "-1", "65536"} {
		if _, err := readTestConfiguration(t, "-tcp-receive-buffer", value); err == nil {
			t.Fatalf("invalid receive buffer %s was accepted", value)
		}
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
	runtime, err := configurationFromPublic(requested, defaults)
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

	runtime, err := configurationFromPublic(requested, defaults)
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
			_, err := configurationFromPublic(requested, defaults)
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

	runtime, err := configurationFromPublic(requested, defaults)
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
	runtime, err = configurationFromPublic(requested, defaults)
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
	cfg.ingress.PacketTravelTime = 13 * time.Nanosecond

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
	if definition.Resources.LoadBalancer.Ingress.PacketTravelTime != cfg.ingress.PacketTravelTime {
		t.Fatal("shared ingress travel time lost nanosecond precision")
	}
}

func TestConfigurationAcceptsExplicitZeros(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	input := `{"clients":0,"servers":0,"bytes":0,"durationSeconds":0,"linkQueue":0,"packetTravelTimeMs":0,"speed":0}`
	if err := json.Unmarshal([]byte(input), &requested); err != nil {
		t.Fatal(err)
	}

	runtime, err := configurationFromPublic(requested, defaults)
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

func TestConfigurationKeepsLogicalTransportLimits(t *testing.T) {
	if math.MaxInt < math.MaxInt64 {
		t.Skip("large stream boundary requires a 64-bit int")
	}
	cfg := legacyTestConfiguration()

	// Validate large byte positions without constructing clients or payloads.
	for _, bytes := range []uint64{
		uint64(math.MaxUint32) + 1,
		uint64(math.MaxUint32)*uint64(model.SEGMENT_SIZE) + 1,
	} {
		cfg.streamBytes = int(bytes)
		if _, err := cfg.normalized(); err != nil {
			t.Fatalf("logical transport rejected %d bytes: %v", bytes, err)
		}
	}

	// Native integer arithmetic still bounds accepted stream sizes.
	cfg.streamBytes = math.MaxInt - 40
	if _, err := cfg.normalized(); err != nil {
		t.Fatal(err)
	}
	cfg.streamBytes = math.MaxInt - 39
	if _, err := cfg.normalized(); err == nil {
		t.Fatal("stream beyond native byte range accepted")
	}

	cfg.streamBytes = 17
	cfg.packets = -1
	if _, err := cfg.normalized(); err == nil {
		t.Fatal("negative packet count accepted with explicit payload size")
	}
}

// TestConfigurationIngressRoundTrip keeps shared input settings separate from outputs.
func TestConfigurationIngressRoundTrip(t *testing.T) {
	cfg := legacyTestConfiguration()
	cfg.bandwidth = 125_000_000
	cfg.forwardingBitsPerSecond = 750_000_000
	cfg.ingress = scenario.LinkConfig{
		BandwidthBitsPerSecond: 500_000_000,
		PacketTravelTime:       1250 * time.Microsecond,
		QueueCapacity:          9,
		LossRate:               0.25,
	}
	requested := cfg.publicConfig()
	data, err := json.Marshal(requested)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"ingressBandwidthBitsPerSecond",
		"ingressPacketTravelTimeMs",
		"ingressQueueCapacity",
		"ingressLoss",
	} {
		if !strings.Contains(string(data), `"`+field+`":`) {
			t.Fatalf("public configuration omitted JSON field %q", field)
		}
	}
	var restored contract.SimulationConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	runtime, err := configurationFromPublic(restored, legacyTestConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ingress != cfg.ingress {
		t.Fatalf("shared ingress changed during the browser round trip: %+v", runtime.ingress)
	}
	definition := runtime.scenarioDefinition()
	if definition.Resources.LoadBalancer.Ingress != cfg.ingress {
		t.Fatal("scenario adapter lost the shared ingress settings")
	}
	if runtime.bandwidth != cfg.bandwidth || runtime.forwardingBitsPerSecond != cfg.forwardingBitsPerSecond {
		t.Fatal("shared ingress mixed endpoint and forwarding bandwidth")
	}
}

// TestConfigurationOmittedIngressFieldsKeepDefaults checks partial WebSocket requests.
func TestConfigurationOmittedIngressFieldsKeepDefaults(t *testing.T) {
	defaults := legacyTestConfiguration()
	defaults.ingress = scenario.LinkConfig{
		BandwidthBitsPerSecond: 300_000_000,
		PacketTravelTime:       2 * time.Millisecond,
		QueueCapacity:          7,
		LossRate:               0.2,
	}
	requested := defaults.publicConfig()
	input := `{"bandwidthBitsPerSecond":125000000,"forwardingBitsPerSecond":750000000}`
	if err := json.Unmarshal([]byte(input), &requested); err != nil {
		t.Fatal(err)
	}
	runtime, err := configurationFromPublic(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ingress != defaults.ingress {
		t.Fatal("omitted ingress settings inherited an endpoint or forwarding limit")
	}

	// Explicit ingress zeros replace nonzero defaults without changing endpoint links.
	requested = defaults.publicConfig()
	input = `{
		"ingressBandwidthBitsPerSecond":250000000,
		"ingressPacketTravelTimeMs":0,
		"ingressQueueCapacity":0,
		"ingressLoss":0
	}`
	if err := json.Unmarshal([]byte(input), &requested); err != nil {
		t.Fatal(err)
	}
	runtime, err = configurationFromPublic(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	wantIngress := scenario.LinkConfig{BandwidthBitsPerSecond: 250_000_000}
	if runtime.ingress != wantIngress {
		t.Fatalf("explicit ingress zeros were replaced by defaults: %+v", runtime.ingress)
	}
	if runtime.bandwidth != defaults.bandwidth || runtime.forwardingBitsPerSecond != defaults.forwardingBitsPerSecond {
		t.Fatal("ingress override changed other bandwidth limits")
	}
	if defaults.ingress.QueueCapacity != 7 || defaults.ingress.PacketTravelTime != 2*time.Millisecond {
		t.Fatal("configuration overlay mutated the original defaults")
	}
}

// TestConfigurationRejectsInvalidIngress checks explicit overrides instead of fallback.
func TestConfigurationRejectsInvalidIngress(t *testing.T) {
	cases := []struct {
		input string
		field string
	}{
		{input: `{"ingressBandwidthBitsPerSecond":0}`, field: "bandwidthBitsPerSecond"},
		{input: `{"ingressBandwidthBitsPerSecond":-1}`, field: "bandwidthBitsPerSecond"},
		{input: `{"ingressQueueCapacity":-1}`, field: "queueCapacity"},
		{input: `{"ingressLoss":-0.1}`, field: "lossRate"},
		{input: `{"ingressLoss":1.1}`, field: "lossRate"},
	}
	for _, test := range cases {
		t.Run(test.input, func(t *testing.T) {
			defaults := legacyTestConfiguration()
			requested := defaults.publicConfig()
			if err := json.Unmarshal([]byte(test.input), &requested); err != nil {
				t.Fatal(err)
			}
			_, err := configurationFromPublic(requested, defaults)
			path := "resources.loadBalancer.ingress." + test.field
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected validation error for %s, got %v", path, err)
			}
		})
	}
}

// TestConfigurationIngressTimeChecksBeforeConversion rejects unsafe float durations.
func TestConfigurationIngressTimeChecksBeforeConversion(t *testing.T) {
	values := []float64{-1, math.NaN(), math.Inf(1), math.MaxFloat64}
	values = append(values, float64(math.MaxInt64)/float64(time.Millisecond))
	for _, value := range values {
		defaults := legacyTestConfiguration()
		requested := defaults.publicConfig()
		requested.IngressPacketTravelTimeMs = value
		_, err := configurationFromPublic(requested, defaults)
		if err == nil || !strings.Contains(err.Error(), "ingressPacketTravelTimeMs") {
			t.Errorf("unsafe ingress duration %v accepted or misidentified: %v", value, err)
		}
	}
}

// TestIngressCLIFlagsKeepIndependentDefaults checks the real flag parser and units.
func TestIngressCLIFlagsKeepIndependentDefaults(t *testing.T) {
	defaults, err := readTestConfiguration(t)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.ingress != scenario.DefaultIngressConfig() || defaults.streamBytes != -1 {
		t.Fatal("CLI lost the common ingress defaults or raw byte sentinel")
	}
	cfg, err := readTestConfiguration(t,
		"-bandwidth", "125000000",
		"-lb-bandwidth", "750000000",
		"-lb-ingress-bandwidth", "300000000",
		"-lb-ingress-packet-travel-time", "13ns",
		"-lb-ingress-queue", "9",
		"-lb-ingress-loss", "0.25",
	)
	if err != nil {
		t.Fatal(err)
	}
	wantIngress := scenario.LinkConfig{
		BandwidthBitsPerSecond: 300_000_000,
		PacketTravelTime:       13 * time.Nanosecond,
		QueueCapacity:          9,
		LossRate:               0.25,
	}
	if cfg.ingress != wantIngress || cfg.bandwidth != 125_000_000 || cfg.forwardingBitsPerSecond != 750_000_000 {
		t.Fatalf("CLI flags mixed shared ingress and output settings: %+v", cfg)
	}
	if _, err := readTestConfiguration(t, "-lb-ingress-bandwidth", "0"); err == nil {
		t.Fatal("an explicit zero CLI ingress bandwidth used the default")
	}
}

// readTestConfiguration isolates process-global flags for these sequential tests.
func readTestConfiguration(t *testing.T, args ...string) (configuration, error) {
	t.Helper()
	previousFlags, previousArgs := flag.CommandLine, os.Args
	defer func() {
		flag.CommandLine, os.Args = previousFlags, previousArgs
	}()
	flag.CommandLine = flag.NewFlagSet("configuration-test", flag.ContinueOnError)
	os.Args = append([]string{"configuration-test"}, args...)
	return readConfiguration()
}
