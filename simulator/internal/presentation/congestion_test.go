package presentation

import (
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/model"
	"load-balancer/scenario"
)

// Each mode projects its actual sender state; reports retain it after routes disappear.
func TestCongestionProjectionAndRetainedReport(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		segments  int
		threshold uint64
	}{
		{name: "defaults", mode: "reno", segments: 4, threshold: 65535},
		{name: "custom Reno", mode: "reno", segments: 2, threshold: 4096},
		{name: "disabled", mode: "none", segments: 7, threshold: 8192},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := projectionScenario()
			definition.Network.Defaults.PacketTravelTime = 5 * time.Millisecond
			definition.Workload.ClientGroups[0].Count = 1
			definition.Workload.Defaults = scenario.StreamConfig{Bytes: 8 * model.SEGMENT_SIZE}
			if test.name != "defaults" {
				definition.TCP.CongestionControl = scenario.CongestionConfig{
					Mode:                           &test.mode,
					InitialWindowSegments:          &test.segments,
					InitialSlowStartThresholdBytes: &test.threshold,
				}
			}
			world := projectionWorld(t, definition)

			// The handshake has returned, but the first data ACK is still travelling.
			advanceProjection(t, world, 25*time.Millisecond)
			var native model.CongestionState
			world.VisitClients(func(client simulation.ClientObservation) {
				native = client.State.Congestion
			})
			if native.BytesInFlight == 0 {
				t.Fatal("expected payload awaiting an ACK")
			}
			sample := project(t, NewProjector("congestion"), world)
			var clientMetrics map[string]float64
			for _, node := range sample.Topology.Nodes {
				if node.ID == "client/clients/0" {
					clientMetrics = node.Metrics
				}
			}
			if clientMetrics["bytesInFlight"] != float64(native.BytesInFlight) {
				t.Fatalf("projected flight = %v, native = %+v", clientMetrics, native)
			}

			// Disabled control omits both limits instead of showing a misleading zero.
			for key, want := range map[string]uint64{
				"congestionWindowBytes":   native.CongestionWindowBytes,
				"slowStartThresholdBytes": native.SlowStartThresholdBytes,
			} {
				value, exists := clientMetrics[key]
				if exists != (test.mode == "reno") || (exists && value != float64(want)) {
					t.Fatalf("projected %s = %v, present = %v, native = %+v", key, value, exists, native)
				}
			}

			// Report configuration records resolved defaults as well as custom/none settings.
			result := BuildResult(world, 0)
			wantConfig := contract.CongestionConfig{
				Mode:                           test.mode,
				InitialWindowSegments:          test.segments,
				InitialSlowStartThresholdBytes: test.threshold,
			}
			info := world.Info().CongestionControl
			if result.TCP.CongestionControl != wantConfig || string(info.Mode) != test.mode ||
				info.InitialWindowSegments != test.segments ||
				info.InitialSlowStartThresholdBytes != test.threshold {
				t.Fatalf("reported config = %+v, world config = %+v", result.TCP.CongestionControl, info)
			}

			advanceProjection(t, world, world.EndTime())
			if err := world.RemoveBackend("one"); err != nil {
				t.Fatal(err)
			}
			finished := BuildResult(world, 0)
			if finished.CompletedStreams != 1 || len(finished.State.Servers) != 0 ||
				len(finished.ClientCongestion) != 1 {
				t.Fatalf("completed report lost the client: %+v", finished.ClientCongestion)
			}
			retained := finished.ClientCongestion[0]
			if retained.ClientID != "client/clients/0" || retained.State.Mode != test.mode ||
				retained.State.BytesInFlight != 0 {
				t.Fatalf("retained sender state = %+v", retained)
			}
			if test.mode == "reno" && retained.State.CongestionWindowBytes == 0 {
				t.Fatal("completed Reno client's window disappeared")
			}
		})
	}
}
