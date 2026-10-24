package main

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/scenario"
)

// trafficTestConfiguration makes a one-byte transmission take exactly 100 ms.
func trafficTestConfiguration() configuration {
	cfg := legacyTestConfiguration()
	cfg.clients = 0
	cfg.servers = 1
	cfg.duration = 150 * time.Millisecond
	cfg.bandwidth = 80
	cfg.forwardingBitsPerSecond = math.MaxInt64
	cfg.packetTravelTime = 20 * time.Millisecond
	return cfg
}

// trafficNode finds the optional measurement on an actual topology endpoint.
func trafficNode(t *testing.T, graph model.Topology, id string) *model.TopologyTraffic {
	t.Helper()
	for _, node := range graph.Nodes {
		if node.ID == id {
			return node.Traffic
		}
	}
	t.Fatalf("missing traffic node %q", id)
	return nil
}

// assertNodeTraffic compares one complete virtual-time measurement window.
func assertNodeTraffic(
	t *testing.T,
	graph model.Topology,
	id string,
	transmit, receive float64,
	window time.Duration,
) {
	t.Helper()
	got := trafficNode(t, graph, id)
	want := model.TopologyTraffic{
		TransmitBytesPerSecond: transmit,
		ReceiveBytesPerSecond:  receive,
		SampleWindowSeconds:    window.Seconds(),
	}
	if got == nil || *got != want {
		t.Fatalf("node %q traffic = %+v, want %+v", id, got, want)
	}
}

// collectTrafficFrames observes the same cached projections sent to the browser.
func collectTrafficFrames(s *simulation) map[time.Duration]model.Topology {
	frames := make(map[time.Duration]model.Topology)
	s.publishState = func() error {
		frames[s.engine.Now()] = s.topology(s.lb.Tick())
		return nil
	}
	return frames
}

// TestTrafficIncludesBoundaryCompletionAndFinalPartialWindow checks both boundaries.
func TestTrafficIncludesBoundaryCompletionAndFinalPartialWindow(t *testing.T) {
	s := newSimulation(trafficTestConfiguration())
	frames := collectTrafficFrames(s)

	// The completion follows enough equal-time events to cross an advance batch.
	for index := 0; index < 5000; index++ {
		s.engine.ScheduleAt(100*time.Millisecond, func() {})
	}
	output := scenarioRuntimeLink(t, s, loadBalancerNodeID, backendNodeID("srv-00")).Output
	arrivals := 0
	output.Transmit(1, func() { arrivals++ })
	if err := s.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Serialization completes exactly at 100 ms; delivery follows at 120 ms.
	first := frames[100*time.Millisecond]
	assertNodeTraffic(t, first, loadBalancerNodeID, 10, 0, 100*time.Millisecond)
	assertNodeTraffic(t, first, backendNodeID("srv-00"), 0, 0, 100*time.Millisecond)
	last := frames[150*time.Millisecond]
	assertNodeTraffic(t, last, loadBalancerNodeID, 0, 0, 50*time.Millisecond)
	assertNodeTraffic(t, last, backendNodeID("srv-00"), 0, 20, 50*time.Millisecond)
	if arrivals != 1 {
		t.Fatalf("delivered callbacks = %d, want 1", arrivals)
	}
}

// TestTrafficLossCountsCompletedTXButNotQueueOrDelivery separates starts from bytes sent.
func TestTrafficLossCountsCompletedTXButNotQueueOrDelivery(t *testing.T) {
	cfg := trafficTestConfiguration()
	cfg.duration = 100 * time.Millisecond
	cfg.queue = 1
	cfg.loss = 1
	s := newSimulation(cfg)
	output := scenarioRuntimeLink(t, s, loadBalancerNodeID, backendNodeID("srv-00")).Output
	arrivals := 0
	for index := 0; index < 3; index++ {
		output.Transmit(1, func() { arrivals++ })
	}
	if err := s.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// One packet finishes and is lost; another starts, and the third was tail-dropped.
	stats := output.Stats()
	if stats.OfferedBytes != 3 || stats.AcceptedBytes != 2 || stats.QueueDroppedBytes != 1 {
		t.Fatalf("unexpected output admission counters: %+v", stats)
	}
	if stats.TransmittedBytes != 1 || stats.LostBytes != 1 || arrivals != 0 {
		t.Fatalf("completed transmission and loss counters disagree: %+v", stats)
	}
	if s.egress.Stats().ForwardedBytes != 2 {
		t.Fatal("fixture did not start its second packet at the end boundary")
	}
	graph := s.topology(s.lb.Tick())
	assertNodeTraffic(t, graph, loadBalancerNodeID, 10, 0, 100*time.Millisecond)
	assertNodeTraffic(t, graph, backendNodeID("srv-00"), 0, 0, 100*time.Millisecond)
}

// TestTrafficBalancerRXFollowsSharedIngress excludes the earlier access arrival.
func TestTrafficBalancerRXFollowsSharedIngress(t *testing.T) {
	cfg := trafficTestConfiguration()
	cfg.duration = 250 * time.Millisecond
	cfg.ingress.BandwidthBitsPerSecond = 80
	cfg.ingress.PacketTravelTime = 30 * time.Millisecond
	s := newSimulation(cfg)
	frames := collectTrafficFrames(s)
	access := scenarioRuntimeLink(t, s, backendNodeID("srv-00"), loadBalancerNodeID).Link
	arrivals := 0
	access.Transmit(1, func() {
		s.backendIngress.Transmit(1, func() { arrivals++ })
	})
	if err := s.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Access finishes TX at 100 ms and reaches the input FIFO at 120 ms.
	first := frames[100*time.Millisecond]
	assertNodeTraffic(t, first, backendNodeID("srv-00"), 10, 0, 100*time.Millisecond)
	middle := frames[200*time.Millisecond]
	assertNodeTraffic(t, middle, loadBalancerNodeID, 0, 0, 100*time.Millisecond)

	// Shared RX finishes at 220 ms, then its travel ends exactly at 250 ms.
	last := frames[250*time.Millisecond]
	assertNodeTraffic(t, last, loadBalancerNodeID, 0, 20, 50*time.Millisecond)
	if arrivals != 1 || s.ingress.Stats().DeliveredBytes != 1 {
		t.Fatal("shared ingress did not deliver exactly one wire byte")
	}
}

// TestTrafficReadsAndDuplicateSamplingKeepStateFrozen checks cached snapshot purity.
func TestTrafficReadsAndDuplicateSamplingKeepStateFrozen(t *testing.T) {
	s := newSimulation(trafficTestConfiguration())
	output := scenarioRuntimeLink(t, s, loadBalancerNodeID, backendNodeID("srv-00")).Output
	output.Transmit(1, func() {})
	if err := s.advance(context.Background(), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	s.sampleTraffic()
	expected := s.topology(s.lb.Tick())
	pending := s.engine.Pending()
	executed := s.engine.EventsExecuted()
	if pending == 0 {
		t.Fatal("fixture needs a travelling packet pending during snapshot reads")
	}

	// Repeated reads and a same-time sample retain the positive measurement window.
	for index := 0; index < 4; index++ {
		s.sampleTraffic()
		if got := s.topology(s.lb.Tick()); !reflect.DeepEqual(got, expected) {
			t.Fatal("snapshot reads or duplicate sampling changed cached traffic")
		}
		s.result(0)
	}
	if s.engine.Pending() != pending || s.engine.EventsExecuted() != executed {
		t.Fatal("traffic snapshot reads scheduled or executed network work")
	}

	// A returned rate is a detached value, even while the model continues to exist.
	trafficNode(t, expected, loadBalancerNodeID).TransmitBytesPerSecond = -1
	assertNodeTraffic(t, s.topology(s.lb.Tick()), loadBalancerNodeID, 10, 0, 100*time.Millisecond)
}

// TestTrafficNewClientUsesSharedWindow checks a client created midway through sampling.
func TestTrafficNewClientUsesSharedWindow(t *testing.T) {
	cfg := trafficTestConfiguration()
	cfg.servers = 0
	cfg.duration = 100 * time.Millisecond
	cfg.bandwidth = 8_000
	cfg.packetTravelTime = 0
	definition := cfg.scenarioDefinition()
	definition.Workload.ClientGroups = []scenario.ClientGroup{{
		ID:      "later",
		Count:   1,
		StartAt: 50 * time.Millisecond,
	}}
	s := newScenarioSimulation(normalizedRuntimeScenario(t, definition))
	if err := s.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Its real 40-byte SYN completes at 90 ms and uses the whole 100 ms window.
	graph := s.topology(s.lb.Tick())
	assertNodeTraffic(t, graph, "client/later/0", 400, 0, 100*time.Millisecond)
	assertNodeTraffic(t, graph, loadBalancerNodeID, 0, 400, 100*time.Millisecond)
}

// TestTrafficHeadlessZeroAndFreshRuns checks availability without a UI publisher.
func TestTrafficHeadlessZeroAndFreshRuns(t *testing.T) {
	cfg := trafficTestConfiguration()
	cfg.headless = true
	s := newSimulation(cfg)
	output := scenarioRuntimeLink(t, s, loadBalancerNodeID, backendNodeID("srv-00")).Output
	output.Transmit(1, func() {})
	if err := s.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	assertNodeTraffic(t, s.topology(s.lb.Tick()), backendNodeID("srv-00"), 0, 20, 50*time.Millisecond)

	// A fresh run has no prior window and later reports a measured idle zero.
	fresh := newSimulation(cfg)
	for _, node := range fresh.topology(fresh.lb.Tick()).Nodes {
		if node.Traffic != nil {
			t.Fatal("fresh model retained a previous run's measurement")
		}
	}
	if err := fresh.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	assertNodeTraffic(t, fresh.topology(fresh.lb.Tick()), loadBalancerNodeID, 0, 0, 50*time.Millisecond)

	// A zero-duration experiment has no positive measurement interval.
	cfg.duration = 0
	zero := newSimulation(cfg)
	if err := zero.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, node := range zero.topology(zero.lb.Tick()).Nodes {
		if node.Traffic != nil {
			t.Fatal("zero-duration run invented a traffic measurement")
		}
	}
}

// TestTrafficFailureSamplesItsActualPartialTime keeps completed work on event limits.
func TestTrafficFailureSamplesItsActualPartialTime(t *testing.T) {
	cfg := trafficTestConfiguration()
	cfg.bandwidth = 160
	cfg.packetTravelTime = 0
	cfg.maxEvents = 1
	s := newSimulation(cfg)
	output := scenarioRuntimeLink(t, s, loadBalancerNodeID, backendNodeID("srv-00")).Output
	output.Transmit(1, func() {})
	err := s.run(context.Background(), nil)
	if !errors.Is(err, engine.ErrEventLimit) {
		t.Fatalf("run error = %v, want event limit", err)
	}
	if s.engine.Now() != 50*time.Millisecond {
		t.Fatalf("failure time = %s, want 50 ms", s.engine.Now())
	}

	// TX finished, but its equal-time delivery remains pending after the failure.
	graph := s.topology(s.lb.Tick())
	assertNodeTraffic(t, graph, loadBalancerNodeID, 20, 0, 50*time.Millisecond)
	assertNodeTraffic(t, graph, backendNodeID("srv-00"), 0, 0, 50*time.Millisecond)
}
