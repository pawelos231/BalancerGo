package main

import (
	"flag"

	"math"
	"os"
	"strconv"

	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

func testConfiguration() configuration {
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

// The CLI source keeps access links, shared ingress and forwarding independent.
func TestConfigurationKeepsIndependentNetworkLimits(t *testing.T) {
	cfg := testConfiguration()
	cfg.bandwidth = 125_000_000
	cfg.ingress.BandwidthBitsPerSecond = 500_000_000
	cfg.forwardingBitsPerSecond = 750_000_000
	prepared, err := cfg.prepareScenarioV2()
	if err != nil {
		t.Fatal(err)
	}
	initial := initialScenarioState(t, prepared.plan)
	if prepared.plan.Fixed().NetworkDefaults.BandwidthBitsPerSecond != cfg.bandwidth ||
		initial.LoadBalancer.Ingress.BandwidthBitsPerSecond != cfg.ingress.BandwidthBitsPerSecond ||
		initial.LoadBalancer.ForwardingBitsPerSecond != cfg.forwardingBitsPerSecond {
		t.Fatal("native configuration mixed independent network limits")
	}
}

// Reject impossible counts before allocating backend slices or creating a world.
func TestScenarioPreparationRejectsCountsBeforeAllocation(t *testing.T) {
	for _, counts := range [][2]int{
		{-1, 2}, {scenario.MaxClients + 1, 2}, {2, -1},
		{2, scenario.MaxBackends + 1}, {2, math.MaxInt},
	} {
		cfg := testConfiguration()
		cfg.clients, cfg.servers = counts[0], counts[1]
		if _, err := cfg.prepareScenarioV2(); err == nil {
			t.Fatalf("invalid counts accepted: %v", counts)
		}
	}
	_, prepared, err := readPreparedTestConfiguration(t, "-servers", strconv.Itoa(math.MaxInt))
	if err == nil || prepared != nil {
		t.Fatal("invalid CLI count retained a prepared scenario")
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
	if defaults.streamBytes != -1 || prepared.definition.Fixed.Workload.Defaults.Bytes != -1 ||
		prepared.definition.Fixed.Workload.Defaults.Packets != 5 || prepared.plan.Fixed().DefaultStream.Bytes != 5*model.SEGMENT_SIZE {
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
			definition: prepared.definition.Fixed.Execution.Duration,
			resolved:   prepared.plan.Fixed().Execution.Duration,
			want:       15 * time.Nanosecond,
		},
		{
			name:       "snapshot interval",
			definition: prepared.definition.Fixed.Execution.SnapshotInterval,
			resolved:   prepared.plan.Fixed().Execution.SnapshotInterval,
			want:       7 * time.Nanosecond,
		},
		{
			name:       "client gap",
			definition: prepared.definition.States["0"].Workload.ClientGroups[0].ClientGap,
			resolved:   initialScenarioState(t, prepared.plan).ClientGroups[0].ClientGap,
			want:       3 * time.Nanosecond,
		},
		{
			name:       "packet gap",
			definition: prepared.definition.Fixed.Workload.Defaults.PacketGap,
			resolved:   prepared.plan.Fixed().DefaultStream.PacketGap,
			want:       5 * time.Nanosecond,
		},
		{
			name:       "packet travel time",
			definition: prepared.definition.Fixed.Network.Defaults.PacketTravelTime,
			resolved:   prepared.plan.Fixed().NetworkDefaults.PacketTravelTime,
			want:       11 * time.Nanosecond,
		},
		{
			name:       "ingress travel time",
			definition: prepared.definition.States["0"].Resources.LoadBalancer.Ingress.PacketTravelTime,
			resolved:   initialScenarioState(t, prepared.plan).LoadBalancer.Ingress.PacketTravelTime,
			want:       13 * time.Nanosecond,
		},
	} {
		if field.definition != field.want || field.resolved != field.want {
			t.Errorf("%s: input=%s runtime=%s, want %s", field.name, field.definition, field.resolved, field.want)
		}
	}
}

func TestScenarioAdapterPreservesExactDurations(t *testing.T) {
	cfg := testConfiguration()
	cfg.duration = 15 * time.Nanosecond
	cfg.snapshotInterval = 7 * time.Nanosecond
	cfg.clientGap = 3 * time.Nanosecond
	cfg.packetGap = 5 * time.Nanosecond
	cfg.packetTravelTime = 11 * time.Nanosecond
	cfg.ingress.PacketTravelTime = 13 * time.Nanosecond

	prepared, err := cfg.prepareScenarioV2()
	if err != nil {
		t.Fatal(err)
	}
	definition := prepared.definition
	if definition.Fixed.Execution.Duration != cfg.duration {
		t.Fatal("experiment duration lost nanosecond precision")
	}
	if definition.Fixed.Execution.SnapshotInterval != cfg.snapshotInterval {
		t.Fatal("snapshot interval changed")
	}
	if definition.States["0"].Workload.ClientGroups[0].ClientGap != cfg.clientGap {
		t.Fatal("client gap changed")
	}
	if definition.Fixed.Workload.Defaults.PacketGap != cfg.packetGap {
		t.Fatal("packet gap changed")
	}
	if definition.Fixed.Network.Defaults.PacketTravelTime != cfg.packetTravelTime {
		t.Fatal("packet travel time changed")
	}
	if definition.States["0"].Resources.LoadBalancer.Ingress.PacketTravelTime != cfg.ingress.PacketTravelTime {
		t.Fatal("shared ingress travel time lost nanosecond precision")
	}
}

func TestHeadlessConfigurationValidatesSpeedBeforeDisablingPacing(t *testing.T) {
	for _, speed := range []float64{-1, math.NaN(), math.Inf(1)} {
		cfg := testConfiguration()
		cfg.headless = true
		cfg.speed = speed
		if _, err := cfg.prepareScenarioV2(); err == nil {
			t.Errorf("invalid headless speed accepted: %v", speed)
		}
	}

	cfg := testConfiguration()
	cfg.headless = true
	cfg.tracePath = "example.trace"
	cfg.speed = 2
	prepared, err := cfg.prepareScenarioV2()
	if err != nil {
		t.Fatal(err)
	}
	if prepared.plan.Fixed().Execution.Speed != 2 || cfg.speed != 2 || !cfg.headless || cfg.tracePath != "example.trace" {
		t.Fatal("headless preparation changed the requested source settings")
	}

	// Invalid playback arithmetic is rejected in the document before either runner starts.
	cfg.duration = time.Duration(math.MaxInt64)
	cfg.speed = 1e-100
	cfg.headless = false
	if _, err := cfg.prepareScenarioV2(); err == nil {
		t.Fatal("paced duration overflow accepted")
	}
}

func TestConfigurationPreservesNativeIntegerLimits(t *testing.T) {
	cfg := testConfiguration()
	cfg.clients = (1 << 24) - 1
	cfg.servers = 0
	cfg.seed = math.MinInt64
	cfg.maxEvents = math.MaxUint64
	cfg.bandwidth = math.MaxInt64
	cfg.forwardingBitsPerSecond = math.MaxInt64
	if _, err := cfg.prepareScenarioV2(); err != nil {
		t.Fatal(err)
	}

	cfg.clients++
	if _, err := cfg.prepareScenarioV2(); err == nil {
		t.Fatal("client address range overflow accepted")
	}
	cfg.clients = 0
	cfg.servers = 10001
	if _, err := cfg.prepareScenarioV2(); err == nil {
		t.Fatal("backend limit exceeded")
	}
}

func TestConfigurationKeepsLogicalTransportLimits(t *testing.T) {
	if math.MaxInt < math.MaxInt64 {
		t.Skip("large stream boundary requires a 64-bit int")
	}
	cfg := testConfiguration()

	// Validate large byte positions without constructing clients or payloads.
	for _, bytes := range []uint64{
		uint64(math.MaxUint32) + 1,
		uint64(math.MaxUint32)*uint64(model.SEGMENT_SIZE) + 1,
	} {
		cfg.streamBytes = int(bytes)
		if _, err := cfg.prepareScenarioV2(); err != nil {
			t.Fatalf("logical transport rejected %d bytes: %v", bytes, err)
		}
	}

	// Native integer arithmetic still bounds accepted stream sizes.
	cfg.streamBytes = math.MaxInt - 40
	if _, err := cfg.prepareScenarioV2(); err != nil {
		t.Fatal(err)
	}
	cfg.streamBytes = math.MaxInt - 39
	if _, err := cfg.prepareScenarioV2(); err == nil {
		t.Fatal("stream beyond native byte range accepted")
	}

	cfg.streamBytes = 17
	cfg.packets = -1
	if _, err := cfg.prepareScenarioV2(); err == nil {
		t.Fatal("negative packet count accepted with explicit payload size")
	}
}

// TestConfigurationIngressRoundTrip keeps shared input settings separate from outputs.
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

func readPreparedTestConfiguration(t *testing.T, args ...string) (configuration, *loadedScenario, error) {
	t.Helper()
	previousFlags, previousArgs := flag.CommandLine, os.Args
	defer func() {
		flag.CommandLine, os.Args = previousFlags, previousArgs
	}()
	flag.CommandLine = flag.NewFlagSet("configuration-test", flag.ContinueOnError)
	os.Args = append([]string{"configuration-test"}, args...)
	config, native, err := readConfiguration()
	if err != nil || native == nil {
		return config, nil, err
	}
	return config, native, nil
}
