package simulation

import (
	"context"
	"time"

	"load-balancer/network"
	"load-balancer/scenario"
)

// baseScenario preserves the small legacy runtime fixture as native input data.
// Application flag parsing and browser configuration are exercised in main tests.
func baseScenario() scenario.Scenario {
	return scenario.Scenario{
		Version:   scenario.Version,
		Execution: scenario.ExecutionConfig{Duration: time.Second, SnapshotInterval: 100 * time.Millisecond, Seed: 1, MaxEvents: 100_000},
		Network:   scenario.NetworkConfig{Defaults: scenario.LinkConfig{BandwidthBitsPerSecond: 1_000_000_000, PacketTravelTime: time.Millisecond, QueueCapacity: 1024}},
		Resources: scenario.ResourcesConfig{
			LoadBalancer: scenario.LoadBalancerConfig{ID: "lb", Algorithm: "roundRobin", Ingress: scenario.DefaultIngressConfig(), ForwardingBitsPerSecond: 1_000_000_000},
			Backends: []scenario.BackendConfig{
				{ID: "srv-00", Name: "backend-00", Address: "10.0.0.1", Port: 80},
				{ID: "srv-01", Name: "backend-01", Address: "10.0.0.2", Port: 80},
			},
		},
		Workload: scenario.WorkloadConfig{Defaults: scenario.StreamConfig{Packets: 3, Bytes: -1}, ClientGroups: []scenario.ClientGroup{{ID: "clients", Count: 2}}},
	}
}

// runWorld advances complete measurement boundaries without application I/O.
func runWorld(ctx context.Context, world *World) error {
	for {
		until := min(world.Now()+world.Info().SnapshotInterval, world.EndTime())
		err := world.Advance(ctx, until)
		world.SampleTraffic()
		if err != nil {
			return err
		}
		if until == world.EndTime() {
			return nil
		}
	}
}

type ingressReadings struct {
	Config network.Config
	network.Stats
	Inputs [2]IngressInputObservation
}

// worldReadings retains all native measurements to compare deterministic runs.
type worldReadings struct {
	Info
	Ingress  ingressReadings
	Clients  []ClientObservation
	Backends []BackendObservation
	Routes   []RouteObservation
	Links    []LinkObservation
	Traffic  map[string]TrafficRate
}

func readWorld(world *World) worldReadings {
	ingress := world.Ingress()
	read := worldReadings{Info: world.Info(), Ingress: ingressReadings{Config: ingress.Config, Stats: ingress.Stats, Inputs: ingress.Inputs}, Traffic: make(map[string]TrafficRate)}
	world.VisitClients(func(value ClientObservation) { read.Clients = append(read.Clients, value) })
	world.VisitBackends(func(value BackendObservation) { read.Backends = append(read.Backends, value) })
	world.VisitRoutes(func(value RouteObservation) { read.Routes = append(read.Routes, value) })
	world.VisitLinks(func(value LinkObservation) {
		read.Links = append(read.Links, value)
		for _, id := range []string{value.Source, value.Target} {
			if rate, known := world.NodeTraffic(id); known {
				read.Traffic[id] = rate
			}
		}
	})
	return read
}
