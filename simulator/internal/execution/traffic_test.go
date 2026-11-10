package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"load-balancer/engine"
	simulationworld "load-balancer/internal/simulation"
	"load-balancer/internal/testsupport"
	"load-balancer/scenario"
)

// runnerTrafficWorld uses a real client's 40-byte SYN to check sampling by the
// application runner without exposing mutable endpoints or physical links.
func runnerTrafficWorld(t *testing.T, bandwidth int64, travel time.Duration, maxEvents uint64) *simulationworld.World {
	t.Helper()
	link := scenario.LinkConfig{BandwidthBitsPerSecond: bandwidth, PacketTravelTime: travel, QueueCapacity: 16}
	definition := testsupport.RuntimeScenario()
	definition.Fixed.Execution = scenario.ExecutionConfig{
		Duration: 150 * time.Millisecond, SnapshotInterval: time.Hour,
		Seed: 1, MaxEvents: maxEvents,
	}
	definition.Fixed.Network.Defaults = link
	initial := definition.States["0"]
	initial.Resources.Backends = nil
	initial.Resources.LoadBalancer.ForwardingBitsPerSecond = 1_000_000_000
	initial.Resources.LoadBalancer.Ingress = scenario.LinkConfig{
		BandwidthBitsPerSecond: 1_000_000_000, QueueCapacity: 16,
	}
	initial.Workload.ClientGroups = []scenario.ClientGroup{{ID: "traffic", Count: 1}}
	definition.States["0"] = initial
	resolved, err := scenario.Compile(definition)
	if err != nil {
		t.Fatal(err)
	}
	return testsupport.NewWorld(resolved)
}

// A 150 ms virtual run finishes before its one-hour real refresh interval.
// Its final snapshot must still include every transmission and arrival.
func TestRunnerTrafficFinalSnapshotIncludesAllBatches(t *testing.T) {
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
	if err := RunWorld(context.Background(), world, publish); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("published %d snapshots, want the final state only", len(frames))
	}
	last := frames[150*time.Millisecond]
	wantRate := 40 / (150 * time.Millisecond).Seconds()
	if last.client.TransmitBytesPerSecond != wantRate || last.client.SampleWindow != 150*time.Millisecond ||
		last.balancer.ReceiveBytesPerSecond != wantRate || last.balancer.SampleWindow != 150*time.Millisecond {
		t.Fatalf("final traffic window = %+v", last)
	}
}

// The first batch stops before the SYN's transmission at the same timestamp.
// Sampling must wait so that the completed 100 ms window includes its 40 bytes.
func TestRunnerTrafficWaitsForCompleteTimestamp(t *testing.T) {
	world := runnerTrafficWorld(t, 3_200, 20*time.Millisecond, 100_000)
	for index := 0; index < 5000; index++ {
		world.ScheduleAt(100*time.Millisecond, func() {})
	}

	reached, err := world.AdvanceBatch(context.Background(), 100*time.Millisecond)
	if err != nil || reached {
		t.Fatalf("first batch reached=%v err=%v, want more work", reached, err)
	}
	world.SampleTraffic()
	if traffic, _ := world.NodeTraffic("client/traffic/0"); traffic.SampleWindow != 0 {
		t.Fatalf("sampled an unfinished timestamp: %+v", traffic)
	}

	if err := world.Advance(context.Background(), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	world.SampleTraffic()
	traffic, known := world.NodeTraffic("client/traffic/0")
	if !known || traffic.TransmitBytesPerSecond != 400 || traffic.SampleWindow != 100*time.Millisecond {
		t.Fatalf("completed timestamp = %+v (known=%v)", traffic, known)
	}
}

// Event limits stop in the middle of the intended boundary. The runner must
// still sample completed work at the engine's actual failure time.
func TestRunnerTrafficFailureSamplesItsActualPartialTime(t *testing.T) {
	world := runnerTrafficWorld(t, 6_400, 0, 3)
	err := RunWorld(context.Background(), world, nil)
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
