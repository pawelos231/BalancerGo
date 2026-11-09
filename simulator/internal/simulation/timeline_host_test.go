package simulation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/scenario"
)

// Disabling CPU flushes waiters after the whole CPU/network cohort is committed.
// Work already using a core still finishes at its original deadline.
func TestTimelineCommitsCPUAndNetworkBeforeResumingWaiters(t *testing.T) {
	definition := timelineDefinition()
	initial := definition.States["0"]
	initial.Resources.LoadBalancer.Host = timelineCPUConfig(1, 10*time.Nanosecond, 2)
	initial.Resources.Backends[0].Host = timelineCPUConfig(1, 10*time.Nanosecond, 2)
	definition.States["0"] = initial

	next := timelineDefinition().States["0"]
	next.Resources.LoadBalancer.Host = timelineCPUConfig(2, 0, 0)
	next.Resources.Backends[0].Host = timelineCPUConfig(2, 0, 0)
	next.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = 4_000_000_000
	next.Resources.LoadBalancer.ForwardingBitsPerSecond = 4_000_000_000
	next.Resources.Backends[0].Network.FromLoadBalancer = scenario.LinkPatch{
		BandwidthBitsPerSecond: scenarioRuntimeValue[int64](16_000_000_000),
	}
	definition.States["5"] = next
	world := newTimelineWorld(t, definition)
	backend := world.lb.GetServers()[0]
	link := scenarioRuntimeLink(t, world, LoadBalancerNodeID, BackendNodeID(backend.ID))

	var completions []string
	packet := model.Packet{Key: model.ClientKey{Protocol: model.TCP}}
	submit := func(name string) {
		backend.HandlePacketIf(packet, func() bool {
			completions = append(completions, name)
			return false
		})
	}
	submit("active")
	submit("first waiter")
	submit("second waiter")
	for range 3 {
		world.lb.HandleClientPacket(packet)
	}
	world.ScheduleAt(5*time.Nanosecond, func() { submit("same-time arrival") })

	world.engine.MaxEvents = 1
	err := world.Advance(context.Background(), 5*time.Nanosecond)
	if !errors.Is(err, engine.ErrEventLimit) || world.EventsExecuted() != 1 {
		t.Fatalf("expected a pause after the configuration event, got %v", err)
	}
	info := world.Info()
	backendCPU := backend.HostStats()
	if info.ConfigurationRevision != 2 || info.LoadBalancerHost.Enabled || backendCPU.Enabled ||
		info.LoadBalancerHost.Cores != 2 || backendCPU.Cores != 2 ||
		info.Forwarding.ForwardingBitsPerSecond != 4_000_000_000 ||
		world.ingress.Config().BandwidthBitsPerSecond != 4_000_000_000 ||
		link.Link.Config().BandwidthBitsPerSecond != 16_000_000_000 {
		t.Fatal("the event budget exposed a partially committed CPU/network configuration")
	}
	if len(completions) != 0 || backendCPU.ProcessedPackets != 0 ||
		backendCPU.BusyCores != 1 || backendCPU.QueuedPackets != 2 ||
		info.LoadBalancerHost.ProcessedPackets != 0 || info.LoadBalancerHost.QueuedPackets != 2 {
		t.Fatal("the configuration callback processed accepted CPU work inline")
	}
	pending := world.PendingEvents()
	if world.Info() != info || backend.HostStats() != backendCPU || world.PendingEvents() != pending {
		t.Fatal("reading a committed configuration resumed pending CPU work")
	}

	world.engine.MaxEvents = 100_000
	if err := world.Advance(context.Background(), 5*time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	want := []string{"first waiter", "second waiter", "same-time arrival"}
	if !reflect.DeepEqual(completions, want) {
		t.Fatalf("same-time arrivals overtook admitted waiters: %v", completions)
	}
	if backend.HostStats().BusyCores != 1 || world.Info().LoadBalancerHost.BusyCores != 1 {
		t.Fatal("disabling CPU removed work that had already started")
	}
	if err := world.Advance(context.Background(), 10*time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	want = append(want, "active")
	if !reflect.DeepEqual(completions, want) || backend.HostStats().BusyTime != 10*time.Nanosecond ||
		backend.HostStats().AvailableCoreTime != 15*time.Nanosecond ||
		world.Info().LoadBalancerHost.ProcessedPackets != 3 {
		t.Fatalf("old CPU work changed deadline or lost history: %+v", backend.HostStats())
	}
}

// A shrink retires occupied cores before taking the next FIFO packet.
// Capacity follows three retiring cores, then one core, then two idle cores.
func TestTimelineCPUTracksRetiringCoresAndIdleCapacity(t *testing.T) {
	definition := timelineDefinition()
	initial := definition.States["0"]
	initial.Resources.Backends[0].Host = timelineCPUConfig(3, 10*time.Nanosecond, 2)
	definition.States["0"] = initial
	shrunken := timelineDefinition().States["0"]
	shrunken.Resources.Backends[0].Host = timelineCPUConfig(1, 2*time.Nanosecond, 0)
	definition.States["5"] = shrunken
	grown := timelineDefinition().States["0"]
	grown.Resources.Backends[0].Host = timelineCPUConfig(2, 2*time.Nanosecond, 0)
	definition.States["14"] = grown
	world := newTimelineWorld(t, definition)
	backend := world.lb.GetServers()[0]
	var completed []time.Duration
	packet := model.Packet{Key: model.ClientKey{Protocol: model.TCP}}
	for range 5 {
		backend.HandlePacketIf(packet, func() bool {
			completed = append(completed, world.Now())
			return false
		})
	}
	if err := world.Advance(context.Background(), 5*time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	state := backend.HostStats()
	if state.Cores != 1 || state.BusyCores != 3 || state.QueuedPackets != 2 {
		t.Fatalf("the shrink discarded admitted work: %+v", state)
	}
	backend.HandlePacketIf(packet, func() bool {
		t.Error("the full shrunken FIFO accepted new work")
		return false
	})
	if backend.HostStats().QueueDroppedPackets != 1 {
		t.Fatal("the shrunken FIFO failed to reject a new waiter")
	}
	if err := world.Advance(context.Background(), 14*time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{
		10 * time.Nanosecond, 10 * time.Nanosecond, 10 * time.Nanosecond,
		12 * time.Nanosecond, 14 * time.Nanosecond,
	}
	state = backend.HostStats()
	if !reflect.DeepEqual(completed, want) || state.BusyTime != 34*time.Nanosecond ||
		state.AvailableCoreTime != 34*time.Nanosecond || state.Utilization() != 1 {
		t.Fatalf("retiring CPU accounting/deadlines changed: completed=%v stats=%+v", completed, state)
	}
	if err := world.Advance(context.Background(), 20*time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	state = backend.HostStats()
	if state.BusyTime != 34*time.Nanosecond || state.AvailableCoreTime != 46*time.Nanosecond ||
		state.Utilization() != 34.0/46.0 {
		t.Fatalf("idle grown capacity used the final core count for past time: %+v", state)
	}
}

func timelineCPUConfig(cores int, cost time.Duration, queue int) scenario.HostConfig {
	return scenario.HostConfig{
		Cores:                &cores,
		PacketProcessingTime: &cost,
		QueueCapacity:        &queue,
	}
}
