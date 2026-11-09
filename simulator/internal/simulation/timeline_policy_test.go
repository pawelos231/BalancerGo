package simulation

import (
	"context"
	"reflect"
	"testing"
	"time"

	"load-balancer/algorithms"
	"load-balancer/client"
	"load-balancer/model"
)

// Directly registered endpoints exercise routing without generating unrelated TCP timers.
func timelinePolicyClient(t *testing.T, world *World, index int) *client.Client {
	t.Helper()
	endpoint := client.NewClient(
		world.engine, "192.0.2.1", uint16(40000+index), "lb.local", 80, model.TCP,
	)
	if err := world.lb.AddClient(endpoint, func(model.Packet) {}); err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func advanceTimelinePolicy(t *testing.T, world *World, at time.Duration) {
	t.Helper()
	if err := world.Advance(context.Background(), at); err != nil {
		t.Fatal(err)
	}
}

// Selection changes affect only new mappings, including packets already using a route.
func TestTimelineAlgorithmChangesKeepExistingRoutes(t *testing.T) {
	definition := timelineDefinition()
	randomState := timelineDefinition().States["0"]
	randomState.Resources.LoadBalancer.Algorithm = "random"
	definition.States["100000000"] = randomState
	definition.States["200000000"] = timelineDefinition().States["0"]
	world := newTimelineWorld(t, definition)
	backends := world.lb.GetServers()
	first := timelinePolicyClient(t, world, 0)
	second := timelinePolicyClient(t, world, 1)
	for index, endpoint := range []*client.Client{first, second} {
		if selected, err := world.lb.Route(endpoint); err != nil || selected != backends[index] {
			t.Fatalf("initial round robin route %d = %v, %v", index, selected, err)
		}
	}

	advanceTimelinePolicy(t, world, 100*time.Millisecond)
	newClient := timelinePolicyClient(t, world, 2)
	want := world.resourceRNG("algorithm").Intn(len(backends))
	if selected, err := world.lb.Route(newClient); err != nil || selected != backends[want] {
		t.Fatalf("new random mapping = %v, %v; want %s", selected, err, backends[want].ID)
	}
	// Repeated SYNs follow their existing backend rather than asking the new selector.
	for _, endpoint := range []*client.Client{first, second} {
		world.lb.HandleClientPacket(model.Packet{Key: endpoint.Key(), Flag: model.FlagSYN})
	}
	advanceTimelinePolicy(t, world, 110*time.Millisecond)
	for index, endpoint := range []*client.Client{first, second} {
		if selected, err := world.lb.Route(endpoint); err != nil || selected != backends[index] {
			t.Fatalf("algorithm change rerouted existing client %d", index)
		}
		if backends[index].GetState().NumberOfHandledPackets == 0 {
			t.Fatalf("existing flow did not reach its original backend %s", backends[index].ID)
		}
	}

	advanceTimelinePolicy(t, world, 200*time.Millisecond)
	for index := range 2 {
		endpoint := timelinePolicyClient(t, world, 3+index)
		if selected, err := world.lb.Route(endpoint); err != nil || selected != backends[index] {
			t.Fatalf("new round robin route %d = %v, %v", index, selected, err)
		}
	}
	if selected, err := world.lb.Route(newClient); err != nil || selected != backends[want] {
		t.Fatal("switching back to round robin replaced a random mapping")
	}
}

// An unrelated edit and an identical declaration preserve selector state and RNG draws.
func TestTimelineAlgorithmStateAndRandomStreamSurviveOtherChanges(t *testing.T) {
	definition := timelineDefinition()
	renamed := timelineDefinition().States["0"]
	renamed.Resources.Backends[0].Name = "renamed"
	definition.States["50000000"] = renamed
	definition.States["75000000"] = renamed
	randomState := timelineDefinition().States["0"]
	randomState.Resources.LoadBalancer.Algorithm = "random"
	definition.States["100000000"] = randomState
	randomWithTimeout := randomState
	randomWithTimeout.Resources.LoadBalancer.MappingTimeouts.ResetIdleTimeout = scenarioRuntimeValue(time.Second)
	definition.States["150000000"] = randomWithTimeout
	definition.States["200000000"] = timelineDefinition().States["0"]
	definition.States["300000000"] = randomState

	var previous []string
	for run := range 2 {
		world := newTimelineWorld(t, definition)
		backends := world.lb.GetServers()
		reference := world.resourceRNG("algorithm")
		var selectedIDs []string
		pick := func(want int) {
			t.Helper()
			endpoint := timelinePolicyClient(t, world, len(selectedIDs))
			selected, err := world.lb.Route(endpoint)
			if err != nil || selected != backends[want] {
				t.Fatalf("selection %d = %v, %v; want %s", len(selectedIDs), selected, err, backends[want].ID)
			}
			selectedIDs = append(selectedIDs, selected.ID)
		}
		initialAlgorithm := world.lb.GetAlgorithm()
		pick(0)
		advanceTimelinePolicy(t, world, 75*time.Millisecond)
		if world.lb.GetAlgorithm() != initialAlgorithm || world.Info().ConfigurationRevision != 2 {
			t.Fatal("a rename or identical state reset the selector or made a redundant commit")
		}
		pick(1)
		advanceTimelinePolicy(t, world, 100*time.Millisecond)
		randomAlgorithm, ok := world.lb.GetAlgorithm().(*algorithms.Random)
		if !ok || randomAlgorithm.RNG != world.algorithmRNG {
			t.Fatal("the random selector does not use the world's existing stream")
		}
		pick(reference.Intn(len(backends)))
		advanceTimelinePolicy(t, world, 150*time.Millisecond)
		if world.lb.GetAlgorithm() != randomAlgorithm {
			t.Fatal("a timeout edit replaced the random selector")
		}
		pick(reference.Intn(len(backends)))
		advanceTimelinePolicy(t, world, 200*time.Millisecond)
		pick(0)
		advanceTimelinePolicy(t, world, 300*time.Millisecond)
		for range 4 {
			pick(reference.Intn(len(backends)))
		}
		if got, want := world.algorithmRNG.Int63(), reference.Int63(); got != want {
			t.Fatal("changing policy reset or consumed the algorithm stream")
		}
		if run > 0 && !reflect.DeepEqual(selectedIDs, previous) {
			t.Fatal("identical timeline seeds produced different selections")
		}
		previous = selectedIDs
	}
}

// Configuration priority includes a cleanup already scheduled at the same timestamp.
func TestTimelineMappingTimeoutsTakeEffectAtTheNextCleanup(t *testing.T) {
	for _, test := range []struct {
		name       string
		changeAt   string
		before     time.Duration
		after      time.Duration
		wantMapped bool
	}{
		{"shorten between sweeps", "4000000000", 0, 3 * time.Second, false},
		{"shorten at sweep", "5000000000", 0, 3 * time.Second, false},
		{"disable at sweep", "5000000000", time.Second, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := timelineDefinition()
			definition.Fixed.Execution.Duration = 11 * time.Second
			initial := definition.States["0"]
			initial.Resources.LoadBalancer.MappingTimeouts.HalfOpenIdleTimeout = &test.before
			definition.States["0"] = initial
			next := initial
			next.Resources.LoadBalancer.MappingTimeouts.HalfOpenIdleTimeout = &test.after
			definition.States[test.changeAt] = next
			world := newTimelineWorld(t, definition)
			advanceTimelinePolicy(t, world, time.Second)
			endpoint := timelinePolicyClient(t, world, 0)
			if _, err := world.lb.Route(endpoint); err != nil {
				t.Fatal(err)
			}
			advanceTimelinePolicy(t, world, 4*time.Second)
			if len(world.lb.GetConnectionTable()[world.lb.GetServers()[0]]) != 1 {
				t.Fatal("changing a timeout cleaned up a mapping before the normal sweep")
			}
			advanceTimelinePolicy(t, world, 5*time.Second)
			assertMapped := func(endpoint *client.Client, want bool) {
				t.Helper()
				mapped := false
				world.VisitRoutes(func(route RouteObservation) { mapped = mapped || route.Key == endpoint.Key() })
				if mapped != want || endpoint.IsTerminated() || !world.lb.HasClient(endpoint) {
					t.Fatalf("mapped = %v, want %v; timeout changed the endpoint or ignored cleanup", mapped, want)
				}
			}
			assertMapped(endpoint, test.wantMapped)
			advanceTimelinePolicy(t, world, 6*time.Second)
			future := timelinePolicyClient(t, world, 1)
			if _, err := world.lb.Route(future); err != nil {
				t.Fatal(err)
			}
			advanceTimelinePolicy(t, world, 10*time.Second)
			assertMapped(future, test.wantMapped)
		})
	}
}
