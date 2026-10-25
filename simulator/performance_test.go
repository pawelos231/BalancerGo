package main

import (
	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestPerformanceUsesWallTime keeps event and packet rates separate from virtual time.
func TestPerformanceUsesWallTime(t *testing.T) {
	got := presentation.MeasurePerformance(2*time.Second, 500, 100)
	want := &contract.SimulationPerformance{
		WallTimeSeconds:               2,
		EventsExecuted:                500,
		EventsPerWallSecond:           250,
		ForwardedPackets:              100,
		ForwardedPacketsPerWallSecond: 50,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("performance = %+v, want %+v", got, want)
	}
}

// TestPerformanceHandlesNonpositiveTime avoids dividing by zero before a clock advances.
func TestPerformanceHandlesNonpositiveTime(t *testing.T) {
	cases := []struct {
		name    string
		elapsed time.Duration
	}{
		{name: "zero", elapsed: 0},
		{name: "negative", elapsed: -time.Nanosecond},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := presentation.MeasurePerformance(test.elapsed, 500, 100)
			want := &contract.SimulationPerformance{
				EventsExecuted:   500,
				ForwardedPackets: 100,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("performance = %+v, want finite zero rates and retained counters", got)
			}
		})
	}
}

// TestPerformanceLifecycle freezes completed measurements and starts new runs at zero.
func TestPerformanceLifecycle(t *testing.T) {
	h := newScenarioHarness(t)
	initial := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "idle"
	})
	if initial.Simulation.Performance != nil {
		t.Fatal("idle state reported execution performance")
	}

	// A short unpaced run still performs real work and reports its own counters.
	input := h.defaults.scenarioDefinition()
	input.Execution.Speed = 0
	input.Execution.Duration = 100 * time.Microsecond
	input.Execution.SnapshotInterval = 10 * time.Microsecond
	input.Execution.MaxEvents = 1_000_000
	input.Network.Defaults.BandwidthBitsPerSecond = 80_000_000_000_000
	input.Network.Defaults.PacketTravelTime = 0
	input.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = 8_000_000_000_000_000
	input.Resources.LoadBalancer.ForwardingBitsPerSecond = 8_000_000_000_000_000
	input.Workload.Defaults.Bytes = 1 << 30
	input.Workload.ClientGroups[0].Count = 4
	receipt := startTelemetryScenario(t, h, input)
	finished := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "finished"
	})
	assertPerformanceCoherent(t, finished)
	if finished.Simulation.Performance.WallTimeSeconds <= 0 {
		t.Fatal("the sustained run has no measurable execution time")
	}
	if finished.Simulation.VirtualTimeSeconds != 0.0001 {
		t.Fatal("the performance clock changed the virtual run horizon")
	}

	// Waiting after completion must not reduce the reported execution rates.
	time.Sleep(20 * time.Millisecond)
	url := "ws" + strings.TrimPrefix(h.server.URL, "http") + "/ws"
	reconnected, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	late := readScenarioSnapshot(t, reconnected, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "finished"
	})
	if !reflect.DeepEqual(late.Simulation.Performance, finished.Simulation.Performance) {
		t.Fatal("reconnect included idle wall time in the finished measurement")
	}

	// Stop removes measurements along with the model and cached graph.
	stopTelemetryScenario(t, h)
	idle := readScenarioSnapshot(t, reconnected, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "idle"
	})
	if idle.Simulation.Performance != nil {
		t.Fatal("Stop retained the previous run's performance")
	}

	// Less work in a fresh model must not inherit the previous cumulative counters.
	input.Workload.ClientGroups[0].Count = 1
	input.Workload.Defaults.Bytes = 0
	second := startTelemetryScenario(t, h, input)
	fresh := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == second.RunID && snapshot.Simulation.Status == "finished"
	})
	assertPerformanceCoherent(t, fresh)
	if second.RunID == receipt.RunID {
		t.Fatal("fresh performance reused the previous run identity")
	}
	if fresh.Simulation.Performance.EventsExecuted >= finished.Simulation.Performance.EventsExecuted {
		t.Fatal("the smaller fresh run retained the previous event count")
	}
	if fresh.Simulation.Performance.ForwardedPackets >= finished.Simulation.Performance.ForwardedPackets {
		t.Fatal("the smaller fresh run retained the previous forwarded packet count")
	}
	stopTelemetryScenario(t, h)
}

// TestPerformanceFailedRunFreezes keeps completed work visible after an event limit.
func TestPerformanceFailedRunFreezes(t *testing.T) {
	h := newScenarioHarness(t)
	input := h.defaults.scenarioDefinition()
	// A tiny playback wait makes elapsed time observable even on coarse clocks.
	input.Execution.Speed = 1
	input.Execution.Duration = time.Millisecond
	input.Execution.SnapshotInterval = time.Millisecond
	input.Execution.MaxEvents = 20
	input.Network.Defaults.PacketTravelTime = 0
	input.Workload.Defaults.Bytes = 1024 * 1024
	receipt := startTelemetryScenario(t, h, input)
	failed := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "failed"
	})
	assertPerformanceCoherent(t, failed)
	if failed.Simulation.Performance.EventsExecuted != 20 {
		t.Fatal("failed measurement lost the executed event limit")
	}
	if !strings.Contains(failed.Simulation.Error, "event execution limit") {
		t.Fatalf("unexpected failure: %q", failed.Simulation.Error)
	}

	// A late subscriber receives the frozen failure rather than an expanding timer.
	time.Sleep(20 * time.Millisecond)
	url := "ws" + strings.TrimPrefix(h.server.URL, "http") + "/ws"
	reconnected, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	late := readScenarioSnapshot(t, reconnected, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "failed"
	})
	if !reflect.DeepEqual(late.Simulation.Performance, failed.Simulation.Performance) {
		t.Fatal("failed measurement grew while waiting for reconnect")
	}
	stopTelemetryScenario(t, h)
}

// assertPerformanceCoherent checks that the owner reports finite real-time rates.
func assertPerformanceCoherent(t *testing.T, snapshot contract.SimulationSnapshot) {
	t.Helper()
	performance := snapshot.Simulation.Performance
	if performance == nil {
		t.Fatal("active model has no execution measurement")
	}
	if performance.WallTimeSeconds < 0 || performance.EventsExecuted == 0 || performance.ForwardedPackets == 0 {
		t.Fatalf("measurement has invalid elapsed time or no completed work: %+v", performance)
	}
	values := []float64{
		performance.WallTimeSeconds,
		performance.EventsPerWallSecond,
		performance.ForwardedPacketsPerWallSecond,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("measurement contains a nonfinite value: %+v", performance)
		}
	}
	if performance.WallTimeSeconds == 0 {
		// Extremely short fresh runs can finish inside one platform clock tick.
		if performance.EventsPerWallSecond != 0 || performance.ForwardedPacketsPerWallSecond != 0 {
			t.Fatal("zero elapsed time produced nonzero rates")
		}
	} else {
		events := performance.EventsPerWallSecond * performance.WallTimeSeconds
		packets := performance.ForwardedPacketsPerWallSecond * performance.WallTimeSeconds
		if math.Abs(events-float64(performance.EventsExecuted)) > 0.000001 {
			t.Fatal("event rate uses a different wall-time denominator")
		}
		if math.Abs(packets-float64(performance.ForwardedPackets)) > 0.000001 {
			t.Fatal("packet rate uses a different wall-time denominator")
		}
	}
	for _, node := range snapshot.Topology.Nodes {
		if node.ID == simulation.LoadBalancerNodeID {
			if uint64(node.Metrics["forwardedPackets"]) != performance.ForwardedPackets {
				t.Fatal("execution packet counter disagrees with the LB forwarding counter")
			}
			return
		}
	}
	t.Fatal("measurement snapshot has no load balancer")
}
