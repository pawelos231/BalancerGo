package simulation_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/internal/simulation"
	"load-balancer/model"
	"load-balancer/scenario"
)

func observationScenario(t *testing.T, clients int) scenario.Normalized {
	t.Helper()
	link := scenario.LinkConfig{BandwidthBitsPerSecond: 8_000_000, QueueCapacity: 32}
	input := scenario.Scenario{
		Version:   scenario.Version,
		Execution: scenario.ExecutionConfig{Duration: time.Second, SnapshotInterval: 100 * time.Millisecond, Seed: 73, MaxEvents: 1_000_000},
		Network:   scenario.NetworkConfig{Defaults: link},
		Resources: scenario.ResourcesConfig{
			LoadBalancer: scenario.LoadBalancerConfig{ID: "router", Algorithm: "random", Ingress: link, ForwardingBitsPerSecond: 8_000_000},
			Backends:     []scenario.BackendConfig{{ID: "second", Address: "10.0.0.2", Port: 80}, {ID: "first", Address: "10.0.0.1", Port: 80}},
		},
		Workload: scenario.WorkloadConfig{
			Defaults:     scenario.StreamConfig{Bytes: 3000, PacketGap: 20 * time.Millisecond},
			ClientGroups: []scenario.ClientGroup{{ID: "clients", Count: clients}},
		},
	}
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

type observations struct {
	Info     simulation.Info
	Ingress  simulation.IngressObservation
	Clients  []simulation.ClientObservation
	Backends []simulation.BackendObservation
	Routes   []simulation.RouteObservation
	Links    []simulation.LinkObservation
	Traffic  map[string]simulation.TrafficRate
}

func readObservations(world *simulation.World) observations {
	read := observations{Info: world.Info(), Ingress: world.Ingress(), Traffic: make(map[string]simulation.TrafficRate)}
	world.VisitClients(func(value simulation.ClientObservation) { read.Clients = append(read.Clients, value) })
	world.VisitBackends(func(value simulation.BackendObservation) { read.Backends = append(read.Backends, value) })
	world.VisitRoutes(func(value simulation.RouteObservation) { read.Routes = append(read.Routes, value) })
	world.VisitLinks(func(value simulation.LinkObservation) { read.Links = append(read.Links, value) })
	for _, link := range read.Links {
		for _, id := range []string{link.Source, link.Target} {
			if rate, known := world.NodeTraffic(id); known {
				read.Traffic[id] = rate
			}
		}
	}
	return read
}

func TestWorldObservationPreservesValuesOrderingAndRandomSequences(t *testing.T) {
	resolved := observationScenario(t, 6)
	inspected, untouched := simulation.New(resolved), simulation.New(resolved)
	for _, world := range []*simulation.World{inspected, untouched} {
		if err := world.Advance(context.Background(), 10*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		world.SampleTraffic()
	}
	before := readObservations(inspected)
	if before.Info.LoadBalancerID != "router" || before.Info.Algorithm != "random" {
		t.Fatalf("run info = %+v", before.Info)
	}
	if got := []string{before.Backends[0].State.ID, before.Backends[1].State.ID}; !reflect.DeepEqual(got, []string{"second", "first"}) {
		t.Fatalf("backend order = %v", got)
	}
	for index, value := range before.Clients {
		if value.Index != index || value.GroupID != "clients" {
			t.Fatalf("client %d = %+v", index, value)
		}
	}
	backendPosition := map[string]int{"second": 0, "first": 1}
	lastBackend := -1
	for _, route := range before.Routes {
		position := backendPosition[route.BackendID]
		if position < lastBackend {
			t.Fatal("route order did not follow backend order")
		}
		lastBackend = position
	}
	for repeat := 0; repeat < 5; repeat++ {
		if after := readObservations(inspected); !reflect.DeepEqual(after, before) {
			t.Fatal("observation changed a world measurement")
		}
	}
	// Mutation of any retained value must not reach the owner-held model.
	copy := readObservations(inspected)
	copy.Info.Algorithm = "changed"
	copy.Clients[0].Config.Stream.Bytes = 1
	copy.Clients[0].State.Key.SrcIP = "changed"
	copy.Backends[0].State.ID = "changed"
	copy.Routes[0].Key.SrcIP = "changed"
	copy.Links[0].Config.QueueCapacity = 0
	copy.Ingress.Inputs[0].Stats.OfferedPackets++
	if after := readObservations(inspected); !reflect.DeepEqual(after, before) {
		t.Fatal("retained observation mutated the world")
	}
	if err := inspected.SpawnClients(3, "clients"); err != nil {
		t.Fatal(err)
	}
	if err := untouched.SpawnClients(3, "clients"); err != nil {
		t.Fatal(err)
	}
	for _, world := range []*simulation.World{inspected, untouched} {
		if err := world.Advance(context.Background(), time.Second); err != nil {
			t.Fatal(err)
		}
		world.SampleTraffic()
	}
	if got, want := readObservations(inspected), readObservations(untouched); !reflect.DeepEqual(got, want) {
		t.Fatal("observations changed later RNG, protocol or sampling results")
	}
	if before.Clients[0].State.Transfer.Status != model.TransferInProgress {
		t.Fatal("retained observation changed after advancing the world")
	}
}

func TestWorldAdvanceCancellationRetainsSameTimeOrder(t *testing.T) {
	resolved := observationScenario(t, 0)
	resolved.Backends = nil
	world := simulation.New(resolved)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls []int
	for index := 0; index < 9000; index++ {
		value := index
		world.ScheduleAt(0, func() {
			calls = append(calls, value)
			if value == 0 {
				cancel()
			}
		})
	}
	if err := world.Advance(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("advance error = %v", err)
	}
	if len(calls) != 4096 || world.EventsExecuted() != 4096 || world.PendingEvents() != 9000-4096 {
		t.Fatalf("cancellation boundary: calls=%d events=%d pending=%d", len(calls), world.EventsExecuted(), world.PendingEvents())
	}
	if err := world.Advance(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 9000 || world.EventsExecuted() != 9000 {
		t.Fatal("resume lost pending callbacks")
	}
	for index, value := range calls {
		if value != index {
			t.Fatalf("callback %d = %d", index, value)
		}
	}
}

func TestWorldEventLimitPreservesPendingCallbacks(t *testing.T) {
	resolved := observationScenario(t, 0)
	resolved.Backends = nil
	resolved.Execution.MaxEvents = 2
	world := simulation.New(resolved)
	for index := 0; index < 3; index++ {
		world.ScheduleAfter(0, func() {})
	}
	if err := world.Advance(context.Background(), time.Second); !errors.Is(err, engine.ErrEventLimit) {
		t.Fatalf("advance error = %v", err)
	}
	if world.Now() != 0 || world.EventsExecuted() != 2 || world.PendingEvents() != 1 {
		t.Fatalf("event-limit state = %+v", world.Info())
	}
}
