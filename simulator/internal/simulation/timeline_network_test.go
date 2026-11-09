package simulation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/network"
	"load-balancer/scenario"
)

// One commit changes both shared limits and a backend before same-time work starts.
func TestTimelineCommitsSharedNetworkLimitsBeforeDispatch(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000_000_000, QueueCapacity: 2,
	}
	initial := definition.States["0"]
	initial.Resources.LoadBalancer.Ingress = definition.Fixed.Network.Defaults
	initial.Resources.LoadBalancer.ForwardingBitsPerSecond = 8_000_000_000
	definition.States["0"] = initial

	next := timelineDefinition().States["0"]
	next.Resources.LoadBalancer.Ingress = scenario.LinkConfig{
		BandwidthBitsPerSecond: 4_000_000_000, LossRate: 1,
	}
	next.Resources.LoadBalancer.ForwardingBitsPerSecond = 4_000_000_000
	next.Resources.Backends[0].Name = "updated with shared limits"
	next.Resources.Backends[0].Network.FromLoadBalancer = scenario.LinkPatch{
		BandwidthBitsPerSecond: scenarioRuntimeValue[int64](16_000_000_000),
		PacketTravelTime:       scenarioRuntimeValue(time.Nanosecond),
		QueueCapacity:          scenarioRuntimeValue(0),
	}
	definition.States["2"] = next
	world := newTimelineWorld(t, definition)
	first := scenarioRuntimeLink(t, world, LoadBalancerNodeID, BackendNodeID("srv-00"))
	second := scenarioRuntimeLink(t, world, LoadBalancerNodeID, BackendNodeID("srv-01"))

	ingressArrivals := 0
	world.clientIngress.Transmit(2, func() { ingressArrivals++ })
	world.backendIngress.Transmit(2, func() { t.Error("new ingress loss must apply") })
	world.clientIngress.Transmit(2, func() { t.Error("new ingress loss must apply") })
	var outputArrivals []time.Duration
	record := func() { outputArrivals = append(outputArrivals, world.Now()) }
	first.Output.Transmit(2, record)
	second.Output.Transmit(2, record)
	first.Output.Transmit(2, record)

	world.engine.MaxEvents = 1
	err := world.Advance(context.Background(), 2*time.Nanosecond)
	if !errors.Is(err, engine.ErrEventLimit) || world.EventsExecuted() != 1 {
		t.Fatalf("expected a pause after the configuration event, got %v", err)
	}
	if world.Info().ConfigurationRevision != 2 ||
		world.Info().Forwarding.ForwardingBitsPerSecond != 4_000_000_000 ||
		world.ingress.Config().BandwidthBitsPerSecond != 4_000_000_000 ||
		world.lb.GetServers()[0].Name != next.Resources.Backends[0].Name ||
		first.Link.Config().BandwidthBitsPerSecond != 16_000_000_000 {
		t.Fatal("the event budget exposed a partially committed network configuration")
	}
	if world.Info().Forwarding.ForwardedPackets != 1 || world.ingress.Stats().TransmittedBytes != 0 {
		t.Fatal("the configuration commit processed queued packets inline")
	}
	for _, input := range []*network.Input{world.clientIngress, world.backendIngress} {
		if input.Transmit(1, func() {}) {
			t.Fatal("a shrunken input FIFO accepted new waiting work")
		}
	}
	if first.Output.Transmit(1, func() {}) {
		t.Fatal("the shrunken output FIFO accepted new waiting work")
	}

	world.engine.MaxEvents = 100_000
	if err := world.Advance(context.Background(), 10*time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{2 * time.Nanosecond, 4 * time.Nanosecond, 8 * time.Nanosecond}
	if !reflect.DeepEqual(outputArrivals, want) {
		t.Fatalf("outputs used stale costs or lost admission order: %v", outputArrivals)
	}
	if ingressArrivals != 1 || world.ingress.Stats().LostPackets != 2 ||
		world.clientIngress.Stats().LostPackets != 1 || world.backendIngress.Stats().LostPackets != 1 {
		t.Fatal("the ingress edit changed active work or lost per-input attribution")
	}
}
