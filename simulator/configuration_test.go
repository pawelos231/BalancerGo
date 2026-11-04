package main

import (
	"encoding/json"
	"flag"
	"load-balancer/internal/contract"
	"math"
	"os"
	"strconv"
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

	prepared, err := cfg.prepareScenario()
	if err != nil {
		t.Fatal(err)
	}
	definition := prepared.definition
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
	restored, err := preparePublicScenario(requested, legacyTestConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if restored.resolved.NetworkDefaults.BandwidthBitsPerSecond != cfg.bandwidth ||
		restored.resolved.LoadBalancer.ForwardingBitsPerSecond != cfg.forwardingBitsPerSecond {
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
	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	definition := prepared.definition
	if definition.TCP.ReceiveBufferBytes == nil || *definition.TCP.ReceiveBufferBytes != 64 {
		t.Fatal("the form overlay lost the TCP receive-buffer capacity")
	}

	for _, invalid := range []int{0, -1, 65536} {
		requested.ReceiveBufferBytes = invalid
		if _, err := preparePublicScenario(requested, defaults); err == nil {
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

func TestConfigurationTCPTimerRoundTrip(t *testing.T) {
	defaults := legacyTestConfiguration()
	requested := defaults.publicConfig()
	if requested.RTOMode != "adaptive" || !requested.RTOBackoff ||
		requested.RTOInitialMs != 1000 || requested.RTOMinimumMs != 200 ||
		requested.RTOMaximumMs != 60000 {
		t.Fatalf("legacy defaults lost their TCP timer policy: %+v", requested)
	}

	// Change timers without changing link limits, and keep false/zero explicit.
	requested.RTOMode = "fixed"
	requested.Bytes = 4096
	requested.RTOBackoff = false
	requested.RTOInitialMs = 200
	requested.RTOMinimumMs = 100
	requested.RTOMaximumMs = 2000
	requested.ClientIdleTimeoutMs = 0
	requested.LBHalfOpenIdleTimeoutMs = 0
	requested.LBEstablishedIdleTimeoutMs = 40000

	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	input := prepared.definition
	if *input.TCP.RTO.ExponentialBackoff || *input.TCP.ClientIdleTimeout != 0 ||
		*input.Resources.LoadBalancer.HalfOpenIdleTimeout != 0 {
		t.Fatal("the retained input lost explicit false or zero timer settings")
	}
	resolved := prepared.resolved
	if resolved.TCP.RTO.Mode != "fixed" || resolved.TCP.RTO.ExponentialBackoff ||
		resolved.TCP.RTO.InitialTimeout != 200*time.Millisecond ||
		resolved.TCP.RTO.MinimumTimeout != 100*time.Millisecond ||
		resolved.TCP.RTO.MaximumTimeout != 2*time.Second ||
		resolved.TCP.ClientIdleTimeout != 0 || resolved.LoadBalancer.HalfOpenIdleTimeout != 0 ||
		resolved.LoadBalancer.EstablishedIdleTimeout != 40*time.Second {
		t.Fatal("the scenario lost explicit timer settings")
	}
	if summary := scenarioSummary(resolved); summary.RTOInitialMs != requested.RTOInitialMs ||
		summary.RTOBackoff != requested.RTOBackoff || summary.ClientIdleTimeoutMs != 0 {
		t.Fatal("scenario summary lost the timer policy")
	}
}

func TestTCPTimerCLIFlags(t *testing.T) {
	// Retry delay and all three cleanup limits remain independently configurable.
	cfg, err := readTestConfiguration(t,
		"-tcp-rto-mode", "fixed",
		"-tcp-rto-backoff=false",
		"-tcp-rto-initial", "200ms",
		"-tcp-rto-minimum", "100ms",
		"-tcp-rto-maximum", "2s",
		"-tcp-idle-timeout", "0",
		"-lb-half-open-idle-timeout", "0",
		"-lb-established-idle-timeout", "40s",
	)
	if err != nil {
		t.Fatal(err)
	}
	rto := cfg.effectiveRTOConfig()
	clientIdle, halfOpenIdle, establishedIdle := cfg.effectiveIdleTimeouts()
	if rto.Mode != model.RTOFixed || rto.ExponentialBackoff ||
		rto.InitialTimeout != 200*time.Millisecond || rto.MinimumTimeout != 100*time.Millisecond ||
		rto.MaximumTimeout != 2*time.Second || clientIdle != 0 || halfOpenIdle != 0 ||
		establishedIdle != 40*time.Second {
		t.Fatal("the CLI ignored independent timer settings")
	}

	invalid := [][]string{
		{"-tcp-rto-mode", ""},
		{"-tcp-rto-initial", "0"},
		{"-tcp-rto-minimum", "2s"},
		{"-tcp-rto-maximum", "500ms"},
		{"-tcp-idle-timeout", "-1s"},
		{"-lb-half-open-idle-timeout", "-1s"},
		{"-lb-established-idle-timeout", "-1s"},
	}
	for _, arguments := range invalid {
		if _, err := readTestConfiguration(t, arguments...); err == nil {
			t.Fatalf("invalid timer flags accepted: %v", arguments)
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
	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.resolved.NetworkDefaults.BandwidthBitsPerSecond != 250_000_000 {
		t.Fatal("explicit link bandwidth override ignored")
	}
	if prepared.resolved.LoadBalancer.ForwardingBitsPerSecond != defaults.forwardingBitsPerSecond {
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

	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.resolved.LoadBalancer.ForwardingBitsPerSecond != 250_000_000 {
		t.Fatal("explicit forwarding budget override ignored")
	}
	if prepared.resolved.NetworkDefaults.BandwidthBitsPerSecond != defaults.bandwidth {
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
			if _, err := cfg.prepareScenario(); err == nil {
				t.Fatal("nonpositive native forwarding budget accepted")
			}

			defaults := legacyTestConfiguration()
			requested := defaults.publicConfig()
			if err := json.Unmarshal([]byte(test.input), &requested); err != nil {
				t.Fatal(err)
			}
			_, err := preparePublicScenario(requested, defaults)
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

	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.resolved.DefaultStream.Bytes != 5*model.SEGMENT_SIZE {
		t.Fatalf("payload bytes = %d", prepared.resolved.DefaultStream.Bytes)
	}
	if defaults.streamBytes != -1 || requested.Bytes != -1 || prepared.definition.Workload.Defaults.Bytes != -1 {
		t.Fatal("normalization changed the unresolved UI/default configuration")
	}

	requested.Bytes = 17
	requested.Packets = 8
	prepared, err = preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.resolved.DefaultStream.Bytes != 17 || prepared.definition.Workload.Defaults.Bytes != 17 {
		t.Fatalf("explicit payload bytes = %d", prepared.resolved.DefaultStream.Bytes)
	}
}

func TestReadConfigurationPreparesRawAndResolvedScenario(t *testing.T) {
	defaults, prepared, err := readPreparedTestConfiguration(t,
		"-packets", "5",
		"-duration", "15ns",
		"-snapshot-interval", "7ns",
		"-client-gap", "3ns",
		"-packet-gap", "5ns",
		"-packet-travel-time", "11ns",
		"-lb-ingress-packet-travel-time", "13ns",
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == nil {
		t.Fatal("CLI settings were not prepared")
	}
	if defaults.streamBytes != -1 || prepared.definition.Workload.Defaults.Bytes != -1 ||
		prepared.definition.Workload.Defaults.Packets != 5 || prepared.resolved.DefaultStream.Bytes != 5*model.SEGMENT_SIZE {
		t.Fatal("preparation lost the raw byte sentinel or resolved the wrong packet count")
	}
	for _, field := range []struct {
		name       string
		definition time.Duration
		resolved   time.Duration
		want       time.Duration
	}{
		{
			name:       "duration",
			definition: prepared.definition.Execution.Duration,
			resolved:   prepared.resolved.Execution.Duration,
			want:       15 * time.Nanosecond,
		},
		{
			name:       "snapshot interval",
			definition: prepared.definition.Execution.SnapshotInterval,
			resolved:   prepared.resolved.Execution.SnapshotInterval,
			want:       7 * time.Nanosecond,
		},
		{
			name:       "client gap",
			definition: prepared.definition.Workload.ClientGroups[0].ClientGap,
			resolved:   prepared.resolved.ClientGroups[0].ClientGap,
			want:       3 * time.Nanosecond,
		},
		{
			name:       "packet gap",
			definition: prepared.definition.Workload.Defaults.PacketGap,
			resolved:   prepared.resolved.DefaultStream.PacketGap,
			want:       5 * time.Nanosecond,
		},
		{
			name:       "packet travel time",
			definition: prepared.definition.Network.Defaults.PacketTravelTime,
			resolved:   prepared.resolved.NetworkDefaults.PacketTravelTime,
			want:       11 * time.Nanosecond,
		},
		{
			name:       "ingress travel time",
			definition: prepared.definition.Resources.LoadBalancer.Ingress.PacketTravelTime,
			resolved:   prepared.resolved.LoadBalancer.Ingress.PacketTravelTime,
			want:       13 * time.Nanosecond,
		},
	} {
		if field.definition != field.want || field.resolved != field.want {
			t.Errorf("%s: input=%s runtime=%s, want %s", field.name, field.definition, field.resolved, field.want)
		}
	}
}

func TestScenarioPreparationRejectsCountsBeforeBackendAllocation(t *testing.T) {
	for _, test := range []struct {
		name    string
		clients int
		servers int
	}{
		{"negative clients", -1, 2},
		{"too many clients", scenario.MaxClients + 1, 2},
		{"negative servers", 2, -1},
		{"too many servers", 2, scenario.MaxBackends + 1},
		{"huge server count", 2, math.MaxInt},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := legacyTestConfiguration()
			cfg.clients, cfg.servers = test.clients, test.servers
			if _, err := cfg.prepareScenario(); err == nil {
				t.Fatal("invalid native count was accepted")
			}
			if _, err := preparePublicScenario(cfg.publicConfig(), legacyTestConfiguration()); err == nil {
				t.Fatal("invalid public count was accepted")
			}
		})
	}

	// This count cannot be used to allocate a backend slice; parsing must return an error first.
	_, prepared, err := readPreparedTestConfiguration(t, "-servers", strconv.Itoa(math.MaxInt))
	if err == nil || prepared != nil {
		t.Fatal("invalid CLI count retained a prepared scenario")
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

	prepared, err := cfg.prepareScenario()
	if err != nil {
		t.Fatal(err)
	}
	definition := prepared.definition
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

	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	resolved := prepared.resolved
	if resolved.TotalClients != 0 || len(resolved.Backends) != 0 || resolved.DefaultStream.Bytes != 0 {
		t.Fatal("zero counts or stream size inherited a default")
	}
	if resolved.Execution.Duration != 0 || resolved.NetworkDefaults.QueueCapacity != 0 ||
		resolved.NetworkDefaults.PacketTravelTime != 0 || resolved.Execution.Speed != 0 {
		t.Fatal("zero resource settings inherited a default")
	}
}

func TestHeadlessConfigurationValidatesSpeedBeforeDisablingPacing(t *testing.T) {
	for _, speed := range []float64{-1, math.NaN(), math.Inf(1)} {
		cfg := legacyTestConfiguration()
		cfg.headless = true
		cfg.speed = speed
		if _, err := cfg.prepareScenario(); err == nil {
			t.Errorf("invalid headless speed accepted: %v", speed)
		}
	}

	cfg := legacyTestConfiguration()
	cfg.headless = true
	cfg.tracePath = "example.trace"
	cfg.duration = time.Duration(math.MaxInt64)
	cfg.speed = 1e-100
	prepared, err := cfg.prepareScenario()
	if err != nil {
		t.Fatal(err)
	}
	if prepared.resolved.Execution.Speed != 0 || cfg.speed != 1e-100 || !cfg.headless || cfg.tracePath != "example.trace" {
		t.Fatal("preparation mutated process options or left headless pacing enabled")
	}

	cfg.headless = false
	if _, err := cfg.prepareScenario(); err == nil {
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
	if _, err := cfg.prepareScenario(); err != nil {
		t.Fatal(err)
	}

	cfg.clients++
	if _, err := cfg.prepareScenario(); err == nil {
		t.Fatal("client address range overflow accepted")
	}
	cfg.clients = 0
	cfg.servers = 10001
	if _, err := cfg.prepareScenario(); err == nil {
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
		if _, err := cfg.prepareScenario(); err != nil {
			t.Fatalf("logical transport rejected %d bytes: %v", bytes, err)
		}
	}

	// Native integer arithmetic still bounds accepted stream sizes.
	cfg.streamBytes = math.MaxInt - 40
	if _, err := cfg.prepareScenario(); err != nil {
		t.Fatal(err)
	}
	cfg.streamBytes = math.MaxInt - 39
	if _, err := cfg.prepareScenario(); err == nil {
		t.Fatal("stream beyond native byte range accepted")
	}

	cfg.streamBytes = 17
	cfg.packets = -1
	if _, err := cfg.prepareScenario(); err == nil {
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
	prepared, err := preparePublicScenario(restored, legacyTestConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.resolved.LoadBalancer.Ingress != cfg.ingress {
		t.Fatalf("shared ingress changed during the browser round trip: %+v", prepared.resolved.LoadBalancer.Ingress)
	}
	definition := prepared.definition
	if definition.Resources.LoadBalancer.Ingress != cfg.ingress {
		t.Fatal("scenario adapter lost the shared ingress settings")
	}
	if prepared.resolved.NetworkDefaults.BandwidthBitsPerSecond != cfg.bandwidth ||
		prepared.resolved.LoadBalancer.ForwardingBitsPerSecond != cfg.forwardingBitsPerSecond {
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
	prepared, err := preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.resolved.LoadBalancer.Ingress != defaults.ingress {
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
	prepared, err = preparePublicScenario(requested, defaults)
	if err != nil {
		t.Fatal(err)
	}
	wantIngress := scenario.LinkConfig{BandwidthBitsPerSecond: 250_000_000}
	if prepared.resolved.LoadBalancer.Ingress != wantIngress {
		t.Fatalf("explicit ingress zeros were replaced by defaults: %+v", prepared.resolved.LoadBalancer.Ingress)
	}
	if prepared.resolved.NetworkDefaults.BandwidthBitsPerSecond != defaults.bandwidth ||
		prepared.resolved.LoadBalancer.ForwardingBitsPerSecond != defaults.forwardingBitsPerSecond {
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
			_, err := preparePublicScenario(requested, defaults)
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
		_, err := preparePublicScenario(requested, defaults)
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
		BandwidthBitsPerSecond:      300_000_000,
		PacketTravelTime:            13 * time.Nanosecond,
		QueueCapacity:               9,
		LossRate:                    0.25,
		CongestionThresholdPercent:  80,
		CongestionSignalProbability: 1,
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
	config, _, err := readPreparedTestConfiguration(t, args...)
	return config, err
}

func readPreparedTestConfiguration(t *testing.T, args ...string) (configuration, *preparedScenario, error) {
	t.Helper()
	previousFlags, previousArgs := flag.CommandLine, os.Args
	defer func() {
		flag.CommandLine, os.Args = previousFlags, previousArgs
	}()
	flag.CommandLine = flag.NewFlagSet("configuration-test", flag.ContinueOnError)
	os.Args = append([]string{"configuration-test"}, args...)
	return readConfiguration()
}
