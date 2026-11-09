package simulation

import (
	"context"
	"time"

	"load-balancer/network"
	"load-balancer/scenario"
)

// baseScenario supplies a small native timeline without application configuration.
// Application flag parsing and browser configuration are exercised in main tests.
func baseScenario() scenario.ScenarioV2 {
	return scenario.ScenarioV2{
		Version: scenario.VersionV2,
		Fixed: scenario.FixedConfig{
			Execution:    scenario.ExecutionConfig{Duration: time.Second, SnapshotInterval: 100 * time.Millisecond, Seed: 1, MaxEvents: 100_000},
			LoadBalancer: scenario.LoadBalancerIdentity{ID: "lb"},
			Network:      scenario.NetworkConfig{Defaults: scenario.LinkConfig{BandwidthBitsPerSecond: 1_000_000_000, PacketTravelTime: time.Millisecond, QueueCapacity: 1024}},
			Workload:     scenario.WorkloadDefaultsConfig{Defaults: scenario.StreamConfig{Packets: 3, Bytes: -1}},
		},
		States: map[string]scenario.StateConfig{"0": {
			Resources: scenario.StateResourcesConfig{
				LoadBalancer: scenario.LoadBalancerStateConfig{Algorithm: "roundRobin", Ingress: scenario.DefaultIngressConfig(), ForwardingBitsPerSecond: 1_000_000_000},
				Backends:     []scenario.BackendStateConfig{{ID: "srv-00", Name: "backend-00", Address: "10.0.0.1", Port: 80}, {ID: "srv-01", Name: "backend-01", Address: "10.0.0.2", Port: 80}},
			},
			Workload: scenario.StateWorkloadConfig{ClientGroups: []scenario.ClientGroup{{ID: "clients", Count: 2}}},
		}},
	}
}

// editInitialState updates only the native document's initial complete state.
func editInitialState(definition *scenario.ScenarioV2, edit func(*scenario.StateConfig)) {
	state := definition.States["0"]
	edit(&state)
	definition.States["0"] = state
}

// runWorld uses fixed virtual measurement windows for model tests, without UI I/O.
func runWorld(ctx context.Context, world *World) error {
	for {
		until := min(world.Now()+100*time.Millisecond, world.EndTime())
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
