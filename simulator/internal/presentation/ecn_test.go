package presentation

import (
	"testing"
	"time"

	"load-balancer/scenario"
)

// Queue marks and TCP reactions must reach both CLI reports and topology frames.
func TestECNProjection(t *testing.T) {
	definition := projectionScenario()
	definition.Execution.Duration = 2 * time.Second
	enabled := true
	definition.TCP.ECNEnabled = &enabled
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 64 * 1024}
	definition.Workload.ClientGroups[0].Count = 4
	definition.Network.Defaults.QueueCapacity = 64
	definition.Network.Defaults.QueuePolicy = "threshold"
	definition.Network.Defaults.CongestionThresholdPercent = 10
	definition.Network.Defaults.CongestionSignalProbability = 1
	// A slow backend output builds a queue without congesting ACK return links.
	bandwidth := int64(1_000_000)
	definition.Resources.Backends[0].Network.FromLoadBalancer.BandwidthBitsPerSecond = &bandwidth
	world := projectionWorld(t, definition)
	advanceProjection(t, world, world.EndTime())

	report := BuildResult(world, 0)
	if !report.TCP.ECNEnabled || len(report.ClientECN) != 4 {
		t.Fatal("the report omitted the enabled ECN profile")
	}
	var reportedMarks, reportedResponses uint64
	for _, link := range report.Links {
		reportedMarks += link.ECNMarkedPackets
	}
	for _, client := range report.ClientECN {
		reportedResponses += client.State.ECNResponseCount
	}
	if reportedMarks == 0 || reportedResponses == 0 {
		t.Fatal("the fixture did not trigger ECN feedback")
	}

	// The UI sees the same marks and reactions through its compact live frame.
	sample := project(t, NewProjector("ecn"), world)
	var topologyMarks uint64
	var topologyResponses float64
	for _, link := range sample.Topology.Links {
		topologyMarks += link.Stats.ECNMarkedPackets
	}
	for _, node := range sample.Topology.Nodes {
		topologyResponses += node.Metrics["ecnResponseCount"]
	}
	if topologyMarks != reportedMarks || topologyResponses != float64(reportedResponses) {
		t.Fatalf("topology marks/responses=%d/%v, report=%d/%d",
			topologyMarks, topologyResponses, reportedMarks, reportedResponses)
	}
}
