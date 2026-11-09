package simulation

import (
	"context"
	"testing"
	"time"

	"load-balancer/scenario"
)

// A queued LB output adopts new settings without replacing its link or losing its queue.
func TestTimelineUpdatesBackendLinksWithoutDiscardingWork(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       10 * time.Millisecond,
		QueueCapacity:          2,
	}
	next := timelineDefinition().States["0"]
	backend := &next.Resources.Backends[0]
	backend.Name = "changed with its links"
	backend.Network.FromLoadBalancer = scenario.LinkPatch{
		BandwidthBitsPerSecond: scenarioRuntimeValue[int64](16_000),
		PacketTravelTime:       scenarioRuntimeValue(20 * time.Millisecond),
		QueueCapacity:          scenarioRuntimeValue(0),
	}
	backend.Network.ToLoadBalancer = scenario.LinkPatch{LossRate: scenarioRuntimeValue(1.0)}
	definition.States["500000"] = next
	world := newTimelineWorld(t, definition)
	toBackend := scenarioRuntimeLink(t, world, LoadBalancerNodeID, BackendNodeID(backend.ID))
	toLB := scenarioRuntimeLink(t, world, BackendNodeID(backend.ID), LoadBalancerNodeID)

	var arrivals []time.Duration
	toBackend.Output.Transmit(1, func() { arrivals = append(arrivals, world.Now()) })
	toBackend.Output.Transmit(1, func() { arrivals = append(arrivals, world.Now()) })
	returned := 0
	toLB.Link.Transmit(1, func() { returned++ })
	toLB.Link.Transmit(1, func() { returned++ })

	if err := world.Advance(context.Background(), 500*time.Microsecond); err != nil {
		t.Fatal(err)
	}
	if toBackend.Link.Config().BandwidthBitsPerSecond != 16_000 || toBackend.Stats().QueuedPackets != 1 {
		t.Fatal("the commit replaced the link or discarded its waiting packet")
	}
	if toBackend.Output.Transmit(1, func() {}) {
		t.Fatal("the shrunken output admitted another waiting packet")
	}
	if world.lb.GetServers()[0].Name != backend.Name || world.Info().ConfigurationRevision != 2 {
		t.Fatal("name and link changes were not committed together")
	}
	if err := world.Advance(context.Background(), 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(arrivals) != 2 || arrivals[0] != 11*time.Millisecond || arrivals[1] != 21500*time.Microsecond {
		t.Fatalf("active and queued packets used the wrong link settings: %v", arrivals)
	}
	if returned != 1 || toLB.Link.Stats().LostPackets != 1 {
		t.Fatal("new loss settings affected the active return packet or missed the queued one")
	}
	if toBackend.Stats().AcceptedPackets != 2 || toBackend.Stats().QueueDroppedPackets != 1 {
		t.Fatal("link reconfiguration reset historical admission counters")
	}
}
