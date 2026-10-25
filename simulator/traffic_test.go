package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"load-balancer/engine"
	simulationworld "load-balancer/internal/simulation"
	"load-balancer/scenario"
)

// runnerTrafficWorld uses a real client's 40-byte SYN to check sampling by the
// application runner without exposing mutable endpoints or physical links.
func runnerTrafficWorld(t *testing.T, bandwidth int64, travel time.Duration, maxEvents uint64) *simulationworld.World {
	t.Helper()
	link := scenario.LinkConfig{BandwidthBitsPerSecond: bandwidth, PacketTravelTime: travel, QueueCapacity: 16}
	definition := scenario.Scenario{
		Version: scenario.Version,
		Execution: scenario.ExecutionConfig{
			Duration: 150 * time.Millisecond, SnapshotInterval: 100 * time.Millisecond,
			Seed: 1, MaxEvents: maxEvents,
		},
		Network: scenario.NetworkConfig{Defaults: link},
		Resources: scenario.ResourcesConfig{LoadBalancer: scenario.LoadBalancerConfig{
			ID: "lb", Algorithm: "roundRobin", ForwardingBitsPerSecond: 1_000_000_000,
			Ingress: scenario.LinkConfig{BandwidthBitsPerSecond: 1_000_000_000, QueueCapacity: 16},
		}},
		Workload: scenario.WorkloadConfig{
			Defaults:     scenario.StreamConfig{Bytes: 0},
			ClientGroups: []scenario.ClientGroup{{ID: "traffic", Count: 1}},
		},
	}
	resolved, err := scenario.Normalize(definition)
	if err != nil {
		t.Fatal(err)
	}
	return simulationworld.New(resolved)
}

// Completion must be sampled after every same-time callback across advance
// batches, and the last publication must use the shorter final window.
func TestRunnerTrafficIncludesBoundaryCompletionAndFinalPartialWindow(t *testing.T) {
	world := runnerTrafficWorld(t, 3_200, 20*time.Millisecond, 100_000)
	for index := 0; index < 5000; index++ {
		world.ScheduleAt(100*time.Millisecond, func() {})
	}
	type frame struct{ client, balancer simulationworld.TrafficRate }
	frames := make(map[time.Duration]frame)
	publish := func() error {
		client, _ := world.NodeTraffic("client/traffic/0")
		balancer, _ := world.NodeTraffic(simulationworld.LoadBalancerNodeID)
		frames[world.Now()] = frame{client: client, balancer: balancer}
		return nil
	}
	if err := runWorld(context.Background(), world, nil, publish, nil); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("published %d boundaries, want 2", len(frames))
	}
	first := frames[100*time.Millisecond]
	if first.client.TransmitBytesPerSecond != 400 || first.client.SampleWindow != 100*time.Millisecond || first.balancer.ReceiveBytesPerSecond != 0 {
		t.Fatalf("100 ms boundary = %+v", first)
	}
	last := frames[150*time.Millisecond]
	if last.client.TransmitBytesPerSecond != 0 || last.client.SampleWindow != 50*time.Millisecond || last.balancer.ReceiveBytesPerSecond != 800 || last.balancer.SampleWindow != 50*time.Millisecond {
		t.Fatalf("final partial boundary = %+v", last)
	}
}

// Event limits stop in the middle of the intended boundary. The runner must
// still sample completed work at the engine's actual failure time.
func TestRunnerTrafficFailureSamplesItsActualPartialTime(t *testing.T) {
	world := runnerTrafficWorld(t, 6_400, 0, 3)
	err := runWorld(context.Background(), world, nil, nil, nil)
	if !errors.Is(err, engine.ErrEventLimit) {
		t.Fatalf("run error = %v, want event limit", err)
	}
	if world.Now() != 50*time.Millisecond {
		t.Fatalf("failure time = %s, want 50 ms", world.Now())
	}
	client, known := world.NodeTraffic("client/traffic/0")
	if !known || client.TransmitBytesPerSecond != 800 || client.SampleWindow != 50*time.Millisecond {
		t.Fatalf("client traffic after event limit = %+v (known=%v)", client, known)
	}
	balancer, known := world.NodeTraffic(simulationworld.LoadBalancerNodeID)
	if !known || balancer.ReceiveBytesPerSecond != 0 || balancer.SampleWindow != 50*time.Millisecond {
		t.Fatalf("balancer traffic after event limit = %+v (known=%v)", balancer, known)
	}
}
