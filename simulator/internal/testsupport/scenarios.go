// Package testsupport provides fresh scenario fixtures shared by integration tests.
package testsupport

import (
	"context"
	_ "embed"
	"encoding/json"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

func Pointer[T any](value T) *T { return &value }

// Scenario is the original two-client, two-backend application test fixture.
// Explicit pointers preserve the source document, including false and zero values.
func Scenario() scenario.Scenario {
	host := func() scenario.HostConfig {
		return scenario.HostConfig{
			Cores:                Pointer(1),
			PacketProcessingTime: Pointer(time.Duration(0)),
			QueueCapacity:        Pointer(1024),
		}
	}
	return scenario.Scenario{
		ScenarioVersion: scenario.CurrentScenarioVersion,
		Fixed: scenario.FixedConfig{
			Execution: scenario.ExecutionConfig{
				Duration:         time.Second,
				SnapshotInterval: 100 * time.Millisecond,
				Seed:             1,
				MaxEvents:        100_000,
			},
			TCP: scenario.TCPConfig{
				MSSBytes:           Pointer(1024),
				ReceiveBufferBytes: Pointer(65535),
				SACKEnabled:        Pointer(false),
				ECNEnabled:         Pointer(false),
				DelayedACK: scenario.DelayedACKConfig{
					Enabled:        Pointer(false),
					SegmentsPerACK: Pointer(2),
					MaxDelay:       Pointer(40 * time.Millisecond),
				},
				RTO: scenario.RTOConfig{
					Mode:               Pointer("adaptive"),
					ExponentialBackoff: Pointer(true),
					InitialTimeout:     Pointer(time.Second),
					MinimumTimeout:     Pointer(200 * time.Millisecond),
					MaximumTimeout:     Pointer(time.Minute),
				},
				Close: scenario.CloseConfig{
					MSL:            Pointer(30 * time.Second),
					FinWaitTimeout: Pointer(time.Minute),
					MaxFINRetries:  Pointer(5),
				},
				CongestionControl: scenario.CongestionConfig{
					Mode:                           Pointer("reno"),
					FastRetransmit:                 Pointer(true),
					CubicFastConvergence:           Pointer(true),
					InitialWindowSegments:          Pointer(4),
					MaximumWindowSegments:          Pointer(0),
					InitialSlowStartThresholdBytes: Pointer(uint64(65535)),
				},
				ClientIdleTimeout:             Pointer(20 * time.Second),
				BackendHalfOpenIdleTimeout:    Pointer(5 * time.Second),
				BackendEstablishedIdleTimeout: Pointer(time.Duration(0)),
			},
			LoadBalancer: scenario.LoadBalancerIdentity{ID: "lb"},
			Network: scenario.NetworkConfig{
				Defaults: scenario.LinkConfig{
					BandwidthBitsPerSecond: 1_000_000_000,
					PacketTravelTime:       time.Millisecond,
					QueueCapacity:          1024,
				},
			},
			Workload: scenario.WorkloadDefaultsConfig{
				Defaults: scenario.StreamConfig{Packets: 3, Bytes: -1},
			},
		},
		States: map[string]scenario.StateConfig{
			"0": {
				Resources: scenario.StateResourcesConfig{
					LoadBalancer: scenario.LoadBalancerStateConfig{
						Algorithm:               "roundRobin",
						Host:                    host(),
						Ingress:                 scenario.DefaultIngressConfig(),
						ForwardingBitsPerSecond: 1_000_000_000,
						MappingTimeouts: scenario.LoadBalancerTimeoutConfig{
							HalfOpenIdleTimeout:    Pointer(5 * time.Second),
							EstablishedIdleTimeout: Pointer(50 * time.Second),
							ClosingIdleTimeout:     Pointer(120 * time.Second),
							ResetIdleTimeout:       Pointer(10 * time.Second),
						},
					},
					Backends: []scenario.BackendStateConfig{
						{ID: "srv-00", Name: "backend-00", Address: "10.0.0.1", Port: 80, Host: host()},
						{ID: "srv-01", Name: "backend-01", Address: "10.0.0.2", Port: 80, Host: host()},
					},
				},
				Workload: scenario.StateWorkloadConfig{ClientGroups: []scenario.ClientGroup{{ID: "clients", Count: 2}}},
			},
		},
	}
}

//go:embed testdata/legacy_summary.json
var legacySummaryJSON []byte

// LegacySummary preserves the previous full telemetry envelope's exact settings.
func LegacySummary() contract.SimulationConfig {
	var summary contract.SimulationConfig
	if err := json.Unmarshal(legacySummaryJSON, &summary); err != nil {
		panic(err)
	}
	return summary
}

func InitialState(t testing.TB, plan *scenario.Compiled) scenario.NormalizedState {
	t.Helper()
	state, err := plan.StateAt(0)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func Compile(t testing.TB, input scenario.Scenario) *scenario.Compiled {
	t.Helper()
	plan, err := scenario.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func NewWorld(input *scenario.Compiled) *simulation.World {
	world, err := simulation.NewTimeline(input)
	if err != nil {
		panic(err)
	}
	return world
}

func Advance(t testing.TB, world *simulation.World, until time.Duration) {
	t.Helper()
	if err := world.Advance(context.Background(), until); err != nil {
		t.Fatal(err)
	}
}

// RuntimeScenario is a small native network for runner and live integration tests.
func RuntimeScenario() scenario.Scenario {
	definition := Scenario()
	definition.Fixed.Execution.Duration = 500 * time.Millisecond
	definition.Fixed.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000_000,
		QueueCapacity:          32,
	}
	initial := definition.States["0"]
	initial.Resources.LoadBalancer.Ingress = definition.Fixed.Network.Defaults
	initial.Resources.LoadBalancer.ForwardingBitsPerSecond = 9_223_372_036_854_775_807
	definition.Fixed.Workload.Defaults = scenario.StreamConfig{Bytes: 0}
	initial.Workload.ClientGroups = []scenario.ClientGroup{{ID: "clients", Count: 1}}
	definition.States["0"] = initial
	return definition
}

// CLIScenario exercises exact large integers and requests playback the CLI ignores.
func CLIScenario() scenario.Scenario {
	input := Scenario()
	input.Fixed.Execution.Duration = 500 * time.Millisecond
	input.Fixed.Network.Defaults = scenario.LinkConfig{BandwidthBitsPerSecond: 8_000_000, QueueCapacity: 32}
	state := input.States["0"]
	state.Resources.LoadBalancer.Ingress = input.Fixed.Network.Defaults
	state.Resources.LoadBalancer.ForwardingBitsPerSecond = 9_223_372_036_854_775_807
	input.Fixed.Workload.Defaults = scenario.StreamConfig{Bytes: 0}
	input.Fixed.Execution.Seed = 9_007_199_254_740_993
	input.Fixed.Execution.Speed = 0.001
	state.Resources.LoadBalancer.Algorithm = "random"
	input.Fixed.Workload.Defaults.Bytes = 2050
	input.Fixed.Network.Profiles = []scenario.LinkProfile{
		{
			ID: "fast",
			Config: scenario.LinkPatch{
				BandwidthBitsPerSecond: Pointer(int64(80_000_000)),
			},
		},
		{
			ID: "slow",
			Config: scenario.LinkPatch{
				BandwidthBitsPerSecond: Pointer(int64(4_000_000)),
				PacketTravelTime:       Pointer(time.Millisecond),
			},
		},
	}
	state.Resources.Backends[0].Network.Profile = "fast"
	state.Resources.Backends[1].Network.Profile = "slow"
	state.Workload.ClientGroups = []scenario.ClientGroup{
		{
			ID:      "fast",
			Count:   2,
			Network: scenario.EndpointNetworkConfig{Profile: "fast"},
			Overrides: []scenario.ClientOverride{
				{
					Index:  1,
					Stream: scenario.StreamPatch{Bytes: Pointer(31)},
				},
			},
		},
		{
			ID:      "slow",
			Count:   1,
			StartAt: 20 * time.Millisecond,
			Network: scenario.EndpointNetworkConfig{Profile: "slow"},
		},
	}
	input.States["0"] = state
	return input
}

// TimelineScenario changes resources and adds a late client group in one transition.
func TimelineScenario() scenario.Scenario {
	input := CLIScenario()
	input.Fixed.Execution.Duration = 120 * time.Millisecond
	input.Fixed.Execution.SnapshotInterval = 5 * time.Millisecond
	later := input.States["0"]
	later.Resources.LoadBalancer.Algorithm = "roundRobin"
	later.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = 16_000_000
	later.Resources.Backends = append([]scenario.BackendStateConfig(nil), later.Resources.Backends...)
	later.Resources.Backends[0].Name = "after-transition"
	later.Workload.ClientGroups = append([]scenario.ClientGroup(nil), later.Workload.ClientGroups...)
	later.Workload.ClientGroups = append(later.Workload.ClientGroups, scenario.ClientGroup{
		ID: "late", Count: 1, StartAt: 40 * time.Millisecond,
		Network: scenario.EndpointNetworkConfig{Profile: "fast"},
		Stream:  scenario.StreamPatch{Bytes: Pointer(19)},
	})
	input.States["40000000"] = later
	return input
}

// PayloadScenario spaces segments apart so reception and ACKs can be observed separately.
func PayloadScenario() scenario.Scenario {
	input := RuntimeScenario()
	{
		initialState := input.States["0"]
		initialState.Resources.Backends = initialState.Resources.Backends[:1]
		input.States["0"] = initialState
	}
	input.Fixed.Network.Defaults.BandwidthBitsPerSecond = 1_000_000_000
	input.Fixed.Network.Defaults.PacketTravelTime = time.Millisecond
	{
		initialState := input.States["0"]
		initialState.Resources.LoadBalancer.Ingress = input.Fixed.Network.Defaults
		input.States["0"] = initialState
	}
	{
		initialState := input.States["0"]
		initialState.Resources.LoadBalancer.Ingress.PacketTravelTime = 0
		input.States["0"] = initialState
	}
	input.Fixed.Workload.Defaults.Bytes = 2050
	input.Fixed.Workload.Defaults.PacketGap = 10 * time.Millisecond
	input.Fixed.Execution.Duration = 2 * time.Second
	return input
}
