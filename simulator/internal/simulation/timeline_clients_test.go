package simulation

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/scenario"
)

func advanceClientTimeline(t *testing.T, world *World, at time.Duration, born int) {
	t.Helper()
	if err := world.Advance(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if got := world.Info().CreatedClients; got != born {
		t.Fatalf("at %s: born = %d, want %d", at, got, born)
	}
	if world.Info().ScheduledClients < born {
		t.Fatal("the planned birth target fell below clients already born")
	}
}

// A budget reduction at an arrival's timestamp wins before that arrival runs.
func TestTimelineClientBudgetPausesAndResumesWithoutRepeatingBirths(t *testing.T) {
	definition := timelineDefinition()
	initial := definition.States["0"]
	initial.Workload.ClientGroups[0].ClientGap = 100 * time.Millisecond
	definition.States["0"] = initial
	for at, count := range map[string]int{
		"100000000": 3,
		"200000000": 1,
		"250000000": 3,
	} {
		next := timelineDefinition().States["0"]
		next.Workload.ClientGroups[0].Count = count
		next.Workload.ClientGroups[0].ClientGap = 100 * time.Millisecond
		definition.States[at] = next
	}
	world := newTimelineWorld(t, definition)
	advanceClientTimeline(t, world, 99*time.Millisecond, 0)
	advanceClientTimeline(t, world, 100*time.Millisecond, 1)
	advanceClientTimeline(t, world, 200*time.Millisecond, 1)
	if world.Info().ScheduledClients != 1 {
		t.Fatal("a reduced budget did not update the planned birth target")
	}
	advanceClientTimeline(t, world, 250*time.Millisecond, 2)
	advanceClientTimeline(t, world, 349*time.Millisecond, 2)
	advanceClientTimeline(t, world, 350*time.Millisecond, 3)
	for index, runtime := range world.clients {
		if runtime.Index != index {
			t.Fatalf("birth %d reused index %d", index, runtime.Index)
		}
	}
}

func TestTimelineClientGapReplansFromLastBirthAndAllowsZeroGap(t *testing.T) {
	definition := timelineDefinition()
	for at, gap := range map[string]time.Duration{
		"0":         100 * time.Millisecond,
		"50000000":  200 * time.Millisecond,
		"210000000": 0,
	} {
		next := timelineDefinition().States["0"]
		next.Workload.ClientGroups[0].Count = 4
		next.Workload.ClientGroups[0].ClientGap = gap
		definition.States[at] = next
	}
	world := newTimelineWorld(t, definition)
	advanceClientTimeline(t, world, 0, 1)
	advanceClientTimeline(t, world, 199*time.Millisecond, 1)
	advanceClientTimeline(t, world, 200*time.Millisecond, 2)
	advanceClientTimeline(t, world, 210*time.Millisecond, 4)
}

func TestTimelineClientGroupRemovalCancelsBirthsAndLetsItsStreamFinish(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Workload.Defaults = scenario.StreamConfig{
		Bytes: 32768, PacketGap: 10 * time.Millisecond,
	}
	initial := definition.States["0"]
	initial.Workload.ClientGroups[0].Count = 2
	initial.Workload.ClientGroups[0].ClientGap = 200 * time.Millisecond
	definition.States["0"] = initial
	removed := timelineDefinition().States["0"]
	removed.Workload.ClientGroups = nil
	definition.States["100000000"] = removed
	world := newTimelineWorld(t, definition)
	advanceClientTimeline(t, world, 50*time.Millisecond, 1)
	endpoint := world.clients[0].Client
	toLB := scenarioRuntimeLink(t, world, world.clients[0].ID, LoadBalancerNodeID)
	advanceClientTimeline(t, world, 100*time.Millisecond, 1)
	if world.clientGroups["clients"].ArrivalEventID != 0 ||
		world.Info().ScheduledClients != 1 || world.clients[0].Client != endpoint ||
		scenarioRuntimeLink(t, world, world.clients[0].ID, LoadBalancerNodeID).Link != toLB.Link {
		t.Fatal("removal kept the generator active or replaced its born client/link")
	}
	advanceClientTimeline(t, world, world.EndTime(), 1)
	if world.Info().CompletedStreams != 1 {
		t.Fatal("removing the generator interrupted its existing TCP stream")
	}
}

// Stream edits affect future births; link edits also reach historical indices.
func TestTimelineClientLinksUpdateWhileBirthStreamsStayFrozen(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Workload.Defaults = scenario.StreamConfig{
		Bytes: 32768, PacketGap: 10 * time.Millisecond,
	}
	initial := definition.States["0"]
	initial.Workload.ClientGroups[0] = scenario.ClientGroup{
		ID: "clients", Count: 3, ClientGap: 100 * time.Millisecond,
		Overrides: []scenario.ClientOverride{{
			Index: 0, Stream: scenario.StreamPatch{Bytes: scenarioRuntimeValue(16384)},
		}},
	}
	definition.States["0"] = initial
	makeState := func(count int, to, from int64) scenario.StateConfig {
		next := timelineDefinition().States["0"]
		next.Workload.ClientGroups[0] = scenario.ClientGroup{
			ID: "clients", Count: count, ClientGap: 100 * time.Millisecond,
			Stream: scenario.StreamPatch{
				Bytes: scenarioRuntimeValue(65536), PacketGap: scenarioRuntimeValue(2 * time.Millisecond),
			},
			Network: scenario.EndpointNetworkConfig{
				ToLoadBalancer:   scenario.LinkPatch{BandwidthBitsPerSecond: &to},
				FromLoadBalancer: scenario.LinkPatch{BandwidthBitsPerSecond: &from},
			},
		}
		return next
	}
	changed := makeState(3, 500_000_000, 400_000_000)
	changed.Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{{
		Index: 0, Stream: scenario.StreamPatch{Bytes: scenarioRuntimeValue(7)},
	}}
	definition.States["50000000"] = changed
	reduced := makeState(1, 600_000_000, 300_000_000)
	reduced.Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{{
		Index:  1,
		Stream: scenario.StreamPatch{Bytes: scenarioRuntimeValue(7)},
		Network: scenario.EndpointNetworkConfig{
			ToLoadBalancer:   scenario.LinkPatch{BandwidthBitsPerSecond: scenarioRuntimeValue[int64](125_000_000)},
			FromLoadBalancer: scenario.LinkPatch{BandwidthBitsPerSecond: scenarioRuntimeValue[int64](250_000_000)},
		},
	}}
	definition.States["150000000"] = reduced
	definition.States["175000000"] = makeState(1, 600_000_000, 300_000_000)
	definition.States["200000000"] = makeState(3, 600_000_000, 300_000_000)
	world := newTimelineWorld(t, definition)
	advanceClientTimeline(t, world, 0, 1)
	pending := world.clientGroups["clients"].ArrivalEventID
	original := world.clients[0].Config.Stream
	toLB := scenarioRuntimeLink(t, world, world.clients[0].ID, LoadBalancerNodeID)
	fromLB := scenarioRuntimeLink(t, world, LoadBalancerNodeID, world.clients[0].ID)
	advanceClientTimeline(t, world, 50*time.Millisecond, 1)
	if world.clientGroups["clients"].ArrivalEventID != pending {
		t.Fatal("a network/stream edit replaced the pending arrival")
	}
	if world.clients[0].Config.Stream != original ||
		toLB.Link.Config().BandwidthBitsPerSecond != 500_000_000 ||
		fromLB.Link.Config().BandwidthBitsPerSecond != 400_000_000 {
		t.Fatal("existing links or the frozen birth stream used the wrong settings")
	}
	advanceClientTimeline(t, world, 100*time.Millisecond, 2)
	if world.clients[1].Config.Stream.Bytes != 65536 {
		t.Fatal("a future client did not inherit the updated stream")
	}
	advanceClientTimeline(t, world, 150*time.Millisecond, 2)
	historical := &world.clients[1]
	historicalTo := scenarioRuntimeLink(t, world, historical.ID, LoadBalancerNodeID)
	historicalFrom := scenarioRuntimeLink(t, world, LoadBalancerNodeID, historical.ID)
	if historicalTo.Link.Config().BandwidthBitsPerSecond != 125_000_000 ||
		historicalFrom.Link.Config().BandwidthBitsPerSecond != 250_000_000 ||
		historical.Config.Stream.Bytes != 65536 {
		t.Fatal("an index above the reduced budget lost its network edit or birth stream")
	}
	advanceClientTimeline(t, world, 175*time.Millisecond, 2)
	if historicalTo.Link.Config().BandwidthBitsPerSecond != 600_000_000 ||
		historicalFrom.Link.Config().BandwidthBitsPerSecond != 300_000_000 ||
		historical.Config.Network.ToLoadBalancer.BandwidthBitsPerSecond != 600_000_000 {
		t.Fatal("removing an override did not restore the current group network")
	}
	advanceClientTimeline(t, world, 200*time.Millisecond, 3)
	advanceClientTimeline(t, world, world.EndTime(), 3)
	if world.Info().CompletedStreams != 3 {
		t.Fatal("the retained and new TCP streams did not finish through the changed links")
	}
}

func TestTimelineClientReservationsCoverFutureGroupsAndKeepRemovedClients(t *testing.T) {
	addresses := func(reverse bool) map[string]string {
		definition := timelineDefinition()
		initial := definition.States["0"]
		initial.Workload.ClientGroups = []scenario.ClientGroup{{ID: "beta", Count: 1}}
		definition.States["0"] = initial
		next := timelineDefinition().States["0"]
		next.Workload.ClientGroups = []scenario.ClientGroup{
			{ID: "beta", Count: 3},
			{ID: "alpha", Count: 1, StartAt: 100 * time.Millisecond},
		}
		if reverse {
			next.Workload.ClientGroups[0], next.Workload.ClientGroups[1] =
				next.Workload.ClientGroups[1], next.Workload.ClientGroups[0]
		}
		definition.States["50000000"] = next
		removed := timelineDefinition().States["0"]
		removed.Workload.ClientGroups = []scenario.ClientGroup{{
			ID: "alpha", Count: 1, StartAt: 100 * time.Millisecond,
		}}
		definition.States["100000000"] = removed
		grown := timelineDefinition().States["0"]
		grown.Workload.ClientGroups = []scenario.ClientGroup{{
			ID: "alpha", Count: 2, StartAt: 100 * time.Millisecond,
		}}
		definition.States["150000000"] = grown
		world := newTimelineWorld(t, definition)
		advanceClientTimeline(t, world, 0, 1)
		oldClient := world.clients[0].Client
		oldLink := scenarioRuntimeLink(t, world, world.clients[0].ID, LoadBalancerNodeID)
		if world.Info().ScheduledClients != 1 {
			t.Fatal("unborn future reservations were reported as planned births")
		}
		advanceClientTimeline(t, world, 50*time.Millisecond, 3)
		advanceClientTimeline(t, world, 100*time.Millisecond, 4)
		if world.clients[0].Client != oldClient || oldClient.IsTerminated() ||
			scenarioRuntimeLink(t, world, world.clients[0].ID, LoadBalancerNodeID).Link != oldLink.Link {
			t.Fatal("stopping a generator replaced or shut down an existing client/link")
		}
		advanceClientTimeline(t, world, 150*time.Millisecond, 5)
		if world.Info().ScheduledClients != 5 {
			t.Fatal("the planned target omitted clients from a removed group")
		}
		result := make(map[string]string)
		for _, runtime := range world.clients {
			result[runtime.ID] = runtime.Client.Key().SrcIP
		}
		return result
	}
	first, reordered := addresses(false), addresses(true)
	want := map[string]string{
		"client/alpha/0": "192.0.0.1", "client/alpha/1": "192.0.0.2",
		"client/beta/0": "192.0.0.3", "client/beta/1": "192.0.0.4",
		"client/beta/2": "192.0.0.5",
	}
	if !reflect.DeepEqual(first, reordered) || !reflect.DeepEqual(first, want) {
		t.Fatalf("client reservations moved or overlapped: %v, reordered %v", first, reordered)
	}
}

func TestTimelineClientArrivalArithmeticStaysInsideTheHorizon(t *testing.T) {
	for _, test := range []struct {
		name  string
		start time.Duration
		count int
		born  int
	}{
		{"gap overflow after a birth", time.Millisecond, 2, 1},
		{"late first birth after a zero budget", time.Duration(math.MaxInt64), 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := timelineDefinition()
			initial := definition.States["0"]
			initial.Workload.ClientGroups[0] = scenario.ClientGroup{
				ID: "clients", Count: test.count, StartAt: test.start, ClientGap: time.Second,
			}
			definition.States["0"] = initial
			next := timelineDefinition().States["0"]
			next.Workload.ClientGroups[0] = scenario.ClientGroup{
				ID: "clients", Count: 2, StartAt: test.start, ClientGap: time.Duration(math.MaxInt64),
			}
			definition.States["2000000"] = next
			world := newTimelineWorld(t, definition)
			advanceClientTimeline(t, world, world.EndTime(), test.born)
		})
	}
}

// Even the largest reservation creates only one pending arrival per present group.
func TestTimelineLargeClientReservationsStayLazy(t *testing.T) {
	makeWorld := func(initialCount, futureCount int) *World {
		definition := timelineDefinition()
		initial := definition.States["0"]
		initial.Workload.ClientGroups = []scenario.ClientGroup{{
			ID: "initial", Count: initialCount, StartAt: 500 * time.Millisecond,
		}}
		definition.States["0"] = initial
		next := timelineDefinition().States["0"]
		next.Workload.ClientGroups = []scenario.ClientGroup{
			initial.Workload.ClientGroups[0],
			{ID: "future", Count: futureCount, StartAt: 750 * time.Millisecond},
		}
		definition.States["100000000"] = next
		return newTimelineWorld(t, definition)
	}
	small := makeWorld(1, 1)
	large := makeWorld(scenario.MaxClients/2, scenario.MaxClients-scenario.MaxClients/2)
	if large.clientGroups["initial"].ArrivalEventID == 0 ||
		large.clientGroups["future"].ArrivalEventID != 0 {
		t.Fatal("construction scheduled a missing group or lost the initial generator")
	}
	for _, world := range []*World{small, large} {
		advanceClientTimeline(t, world, 100*time.Millisecond, 0)
		for _, group := range world.clientGroupOrder {
			if group.ArrivalEventID == 0 || len(group.ClientIndices) != 0 {
				t.Fatal("a future birth has no arrival or already constructed its client")
			}
		}
	}
	if len(large.clients) != 0 || len(large.links) != len(small.links) ||
		large.PendingEvents() != small.PendingEvents() ||
		large.Info().ScheduledClients != scenario.MaxClients {
		t.Fatal("large reservations expanded into clients, links or individual events")
	}
	large.Stop()
	if large.PendingEvents() != 0 {
		t.Fatal("Stop kept a pending generator callback")
	}
}
