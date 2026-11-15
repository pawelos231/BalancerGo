package simulation

import (
	"reflect"
	"testing"
	"time"

	"load-balancer/network"
	"load-balancer/randomness"
	"load-balancer/scenario"
)

// Reading measurements between real routing and network operations must not
// consume selection, loss or congestion draws from the world's registry.
func TestWorldObservationsPreserveRandomStreams(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000_000_000, QueueCapacity: 64, LossRate: 0.4,
		QueuePolicy: "threshold", CongestionThresholdPercent: 1, CongestionSignalProbability: 0.5,
	}
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.Algorithm = "random"
	})
	type arrival struct {
		at     time.Duration
		marked bool
	}
	type observed struct {
		selected []string
		arrivals []arrival
		readings worldReadings
	}
	var baseline observed
	for _, inspect := range []bool{false, true} {
		world := newTimelineWorld(t, definition)
		if world.randomGenerators.Stream("algorithm", randomness.Selection) != world.algorithmRNG {
			t.Fatal("the selector is not using the world's cached stream")
		}
		link := world.backends[world.lb.GetServers()[0].ID].returnLink
		var result observed
		for burst := range 4 {
			for index := range 16 {
				if inspect {
					readWorld(world)
					world.CurrentConfiguration()
				}
				endpoint := timelinePolicyClient(t, world, burst*16+index)
				selected, err := world.lb.Route(endpoint)
				if err != nil {
					t.Fatal(err)
				}
				result.selected = append(result.selected, selected.ID)
				link.TransmitECN(1, network.Capable, func(marked bool) {
					result.arrivals = append(result.arrivals, arrival{world.Now(), marked})
				})
			}
			advanceTimelinePolicy(t, world, time.Duration(burst+1)*100*time.Nanosecond)
			world.SampleTraffic()
			if inspect {
				readWorld(world)
			}
		}
		stats := link.Stats()
		if stats.LostPackets == 0 || stats.ECNMarkedPackets == 0 || len(result.arrivals) == 0 {
			t.Fatalf("fixture did not exercise both network phenomena: %+v", stats)
		}
		result.readings = readWorld(world)
		if inspect {
			if !reflect.DeepEqual(result, baseline) {
				t.Fatal("measurement reads changed seeded routing, network outcomes or final state")
			}
		} else {
			baseline = result
		}
	}
}
