package simulation

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/scenario"
)

// trafficTestDefinition makes a one-byte transmission take exactly 100 ms.
func trafficTestDefinition() scenario.Scenario {
	definition := baseScenario()
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Workload.ClientGroups[0].Count = 0
		state.Resources.Backends = state.Resources.Backends[:1]
	})
	definition.Fixed.Execution.Duration = 150 * time.Millisecond
	definition.Fixed.Network.Defaults.BandwidthBitsPerSecond = 80
	definition.Fixed.Network.Defaults.PacketTravelTime = 20 * time.Millisecond
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.ForwardingBitsPerSecond = math.MaxInt64
	})
	return definition
}

func assertNodeTraffic(t *testing.T, rates map[string]TrafficRate, id string, transmit, receive float64, window time.Duration) {
	t.Helper()
	got, known := rates[id]
	want := TrafficRate{TransmitBytesPerSecond: transmit, ReceiveBytesPerSecond: receive, SampleWindow: window}
	if !known || got != want {
		t.Fatalf("node %q traffic = %+v (known=%v), want %+v", id, got, known, want)
	}
}

// trafficBoundaries observes detached measurements after complete DES advances.
// The application runner's choice of boundaries is tested in the main package.
func trafficBoundaries(t *testing.T, world *World, boundaries ...time.Duration) map[time.Duration]map[string]TrafficRate {
	t.Helper()
	frames := make(map[time.Duration]map[string]TrafficRate)
	for _, until := range boundaries {
		if err := world.Advance(context.Background(), until); err != nil {
			t.Fatal(err)
		}
		world.SampleTraffic()
		frames[world.Now()] = readWorld(world).Traffic
	}
	return frames
}

func TestTrafficIncludesBoundaryCompletionAndFinalPartialWindow(t *testing.T) {
	s := newTimelineWorld(t, trafficTestDefinition())
	for index := 0; index < 5000; index++ {
		s.engine.ScheduleAt(100*time.Millisecond, func() {})
	}
	output := scenarioRuntimeLink(t, s, LoadBalancerNodeID, BackendNodeID("srv-00")).Output
	arrivals := 0
	output.Transmit(1, func() { arrivals++ })
	frames := trafficBoundaries(t, s, 100*time.Millisecond, 150*time.Millisecond)

	// Serialization finishes at 100 ms; delivery follows at 120 ms.
	assertNodeTraffic(t, frames[100*time.Millisecond], LoadBalancerNodeID, 10, 0, 100*time.Millisecond)
	assertNodeTraffic(t, frames[100*time.Millisecond], BackendNodeID("srv-00"), 0, 0, 100*time.Millisecond)
	assertNodeTraffic(t, frames[150*time.Millisecond], LoadBalancerNodeID, 0, 0, 50*time.Millisecond)
	assertNodeTraffic(t, frames[150*time.Millisecond], BackendNodeID("srv-00"), 0, 20, 50*time.Millisecond)
	if arrivals != 1 {
		t.Fatalf("delivered callbacks = %d, want 1", arrivals)
	}
}

func TestTrafficLossCountsCompletedTXButNotQueueOrDelivery(t *testing.T) {
	definition := trafficTestDefinition()
	definition.Fixed.Execution.Duration = 100 * time.Millisecond
	definition.Fixed.Network.Defaults.QueueCapacity = 1
	definition.Fixed.Network.Defaults.LossRate = 1
	s := newTimelineWorld(t, definition)
	output := scenarioRuntimeLink(t, s, LoadBalancerNodeID, BackendNodeID("srv-00")).Output
	arrivals := 0
	for index := 0; index < 3; index++ {
		output.Transmit(1, func() { arrivals++ })
	}
	if err := runWorld(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	stats := output.Stats()
	if stats.OfferedBytes != 3 || stats.AcceptedBytes != 2 || stats.QueueDroppedBytes != 1 {
		t.Fatalf("unexpected output admission counters: %+v", stats)
	}
	if stats.TransmittedBytes != 1 || stats.LostBytes != 1 || arrivals != 0 {
		t.Fatalf("completed transmission and loss counters disagree: %+v", stats)
	}
	if s.Info().Forwarding.ForwardedBytes != 2 {
		t.Fatal("fixture did not start its second packet at the end boundary")
	}
	rates := readWorld(s).Traffic
	assertNodeTraffic(t, rates, LoadBalancerNodeID, 10, 0, 100*time.Millisecond)
	assertNodeTraffic(t, rates, BackendNodeID("srv-00"), 0, 0, 100*time.Millisecond)
}

func TestTrafficBalancerRXFollowsSharedIngress(t *testing.T) {
	definition := trafficTestDefinition()
	definition.Fixed.Execution.Duration = 250 * time.Millisecond
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = 80

		state.Resources.LoadBalancer.Ingress.PacketTravelTime = 30 * time.Millisecond
	})
	s := newTimelineWorld(t, definition)
	access := scenarioRuntimeLink(t, s, BackendNodeID("srv-00"), LoadBalancerNodeID).Link
	arrivals := 0
	access.Transmit(1, func() {
		s.backendIngress.Transmit(1, func() { arrivals++ })
	})
	frames := trafficBoundaries(t, s, 100*time.Millisecond, 200*time.Millisecond, 250*time.Millisecond)

	assertNodeTraffic(t, frames[100*time.Millisecond], BackendNodeID("srv-00"), 10, 0, 100*time.Millisecond)
	assertNodeTraffic(t, frames[200*time.Millisecond], LoadBalancerNodeID, 0, 0, 100*time.Millisecond)
	// Shared RX finishes at 220 ms; its travel ends exactly at 250 ms.
	assertNodeTraffic(t, frames[250*time.Millisecond], LoadBalancerNodeID, 0, 20, 50*time.Millisecond)
	if arrivals != 1 || s.Ingress().Stats.DeliveredBytes != 1 {
		t.Fatal("shared ingress did not deliver exactly one wire byte")
	}
}

func TestTrafficReadsAndDuplicateSamplingKeepStateFrozen(t *testing.T) {
	s := newTimelineWorld(t, trafficTestDefinition())
	output := scenarioRuntimeLink(t, s, LoadBalancerNodeID, BackendNodeID("srv-00")).Output
	output.Transmit(1, func() {})
	if err := s.Advance(context.Background(), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	s.SampleTraffic()
	expected := readWorld(s)
	pending, executed := s.PendingEvents(), s.EventsExecuted()
	if pending == 0 {
		t.Fatal("fixture needs a travelling packet pending during observations")
	}
	for index := 0; index < 4; index++ {
		s.SampleTraffic()
		if got := readWorld(s); !reflect.DeepEqual(got, expected) {
			t.Fatal("observations or duplicate sampling changed cached traffic")
		}
	}
	if s.PendingEvents() != pending || s.EventsExecuted() != executed {
		t.Fatal("traffic observations scheduled or executed network work")
	}

	// A retained rate is detached from the owner's cached measurement.
	rate := expected.Traffic[LoadBalancerNodeID]
	rate.TransmitBytesPerSecond = -1
	expected.Traffic[LoadBalancerNodeID] = rate
	assertNodeTraffic(t, readWorld(s).Traffic, LoadBalancerNodeID, 10, 0, 100*time.Millisecond)
}

func TestTrafficNewClientUsesSharedWindow(t *testing.T) {
	definition := trafficTestDefinition()
	editInitialState(&definition, func(state *scenario.StateConfig) { state.Resources.Backends = nil })
	definition.Fixed.Execution.Duration = 100 * time.Millisecond
	definition.Fixed.Network.Defaults.BandwidthBitsPerSecond = 8_000
	definition.Fixed.Network.Defaults.PacketTravelTime = 0
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Workload.ClientGroups = []scenario.ClientGroup{{ID: "later", Count: 1, StartAt: 50 * time.Millisecond}}
	})
	s := newTimelineWorld(t, definition)
	if err := runWorld(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	// Its real 40-byte SYN finishes at 90 ms and uses the shared 100 ms window.
	rates := readWorld(s).Traffic
	assertNodeTraffic(t, rates, "client/later/0", 400, 0, 100*time.Millisecond)
	assertNodeTraffic(t, rates, LoadBalancerNodeID, 0, 400, 100*time.Millisecond)
}

func TestTrafficHeadlessAndFreshRuns(t *testing.T) {
	definition := trafficTestDefinition()
	s := newTimelineWorld(t, definition)
	output := scenarioRuntimeLink(t, s, LoadBalancerNodeID, BackendNodeID("srv-00")).Output
	output.Transmit(1, func() {})
	if err := runWorld(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	assertNodeTraffic(t, readWorld(s).Traffic, BackendNodeID("srv-00"), 0, 20, 50*time.Millisecond)

	fresh := newTimelineWorld(t, definition)
	if len(readWorld(fresh).Traffic) != 0 {
		t.Fatal("fresh world retained a previous run's measurement")
	}
	if err := runWorld(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	assertNodeTraffic(t, readWorld(fresh).Traffic, LoadBalancerNodeID, 0, 0, 50*time.Millisecond)

	unadvanced := newTimelineWorld(t, definition)
	unadvanced.SampleTraffic()
	if len(readWorld(unadvanced).Traffic) != 0 {
		t.Fatal("an unadvanced world invented a traffic measurement")
	}
}

func TestTrafficFailureSamplesItsActualPartialTime(t *testing.T) {
	definition := trafficTestDefinition()
	definition.Fixed.Network.Defaults.BandwidthBitsPerSecond = 160
	definition.Fixed.Network.Defaults.PacketTravelTime = 0
	definition.Fixed.Execution.MaxEvents = 1
	s := newTimelineWorld(t, definition)
	output := scenarioRuntimeLink(t, s, LoadBalancerNodeID, BackendNodeID("srv-00")).Output
	output.Transmit(1, func() {})
	err := s.Advance(context.Background(), 100*time.Millisecond)
	if !errors.Is(err, engine.ErrEventLimit) {
		t.Fatalf("advance error = %v, want event limit", err)
	}
	s.SampleTraffic()
	if s.Now() != 50*time.Millisecond {
		t.Fatalf("failure time = %s, want 50 ms", s.Now())
	}
	// TX finished, while its equal-time delivery remains pending after failure.
	rates := readWorld(s).Traffic
	assertNodeTraffic(t, rates, LoadBalancerNodeID, 20, 0, 50*time.Millisecond)
	assertNodeTraffic(t, rates, BackendNodeID("srv-00"), 0, 0, 50*time.Millisecond)
}
