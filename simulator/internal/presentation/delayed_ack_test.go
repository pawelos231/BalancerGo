package presentation

import (
	"testing"
	"time"

	"load-balancer/scenario"
)

// The CLI report and topology frame expose the same ACK counts and effective cap.
func TestDelayedACKReportAndTopology(t *testing.T) {
	definition := projectionScenario()
	definition.Execution.Duration = time.Second
	definition.Workload.ClientGroups[0].Count = 1
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 4096}
	enabled, capSegments := true, 1
	definition.TCP.DelayedACK.Enabled = &enabled
	definition.TCP.CongestionControl.MaximumWindowSegments = &capSegments
	world := projectionWorld(t, definition)
	advanceProjection(t, world, world.EndTime())

	report := BuildResult(world, 0)
	config := report.TCP.DelayedACK
	if !config.Enabled || config.SegmentsPerACK != 2 || config.MaxDelay != 40*time.Millisecond {
		t.Fatalf("report lost the effective ACK policy: %+v", config)
	}
	if report.TCP.CongestionControl.MaximumWindowSegments != 1 {
		t.Fatalf("report lost the one-MSS window cap: %+v", report.TCP.CongestionControl)
	}
	ack := report.State.Servers[0].Server.ACK
	if ack == nil || ack.SentACKCount != 4 || ack.TimerTriggeredACKCount != 4 {
		t.Fatalf("report lost the four timer-triggered ACKs: %+v", ack)
	}

	// The graph uses the same counters, without requiring a second model pass.
	sample := project(t, NewProjector("delayed-ack"), world)
	found := false
	for _, node := range sample.Topology.Nodes {
		if node.ID == "backend/one" {
			found = true
			if node.Metrics["sentACKCount"] != 4 || node.Metrics["timerTriggeredACKCount"] != 4 {
				t.Fatalf("topology ACK metrics = %+v", node.Metrics)
			}
		}
	}
	if !found {
		t.Fatal("backend node is missing")
	}
}
