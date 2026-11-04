package simulation

import (
	"context"
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

// Only backend-facing links congest. ACK return paths remain unconstrained,
// so CE feedback can slow clients without losing the transfer's payload.
func ecnScenario() scenario.Scenario {
	input := baseScenario()
	input.Execution.Duration = 10 * time.Second
	input.Execution.MaxEvents = 1_000_000
	input.TCP.ECNEnabled = scenarioRuntimeValue(true)
	input.Workload.ClientGroups[0].Count = 4
	input.Workload.Defaults = scenario.StreamConfig{Bytes: 32 * 1024, PacketGap: 5 * time.Millisecond}
	input.Resources.Backends = input.Resources.Backends[:1]
	input.Resources.Backends[0].Network.FromLoadBalancer = scenario.LinkPatch{
		BandwidthBitsPerSecond:      scenarioRuntimeValue(int64(1_000_000)),
		QueueCapacity:               scenarioRuntimeValue(64),
		QueuePolicy:                 scenarioRuntimeValue("threshold"),
		CongestionThresholdPercent:  scenarioRuntimeValue(25.0),
		CongestionSignalProbability: scenarioRuntimeValue(1.0),
	}
	return input
}

func TestECNTransferThroughWorldQueues(t *testing.T) {
	for _, mode := range []string{"reno", "newReno", "tahoe", "cubic"} {
		t.Run(mode, func(t *testing.T) {
			input := ecnScenario()
			input.TCP.CongestionControl.Mode = scenarioRuntimeValue(mode)
			world := New(normalizedRuntimeScenario(t, input))
			if err := world.Advance(context.Background(), world.EndTime()); err != nil {
				t.Fatal(err)
			}
			info := world.Info()
			wantBytes := uint64(4 * 32 * 1024)
			if info.CompletedStreams != 4 || info.Payload.ReceivedBytes != wantBytes || info.Payload.AcknowledgedBytes != wantBytes {
				t.Fatalf("payload did not finish: completed=%d payload=%+v", info.CompletedStreams, info.Payload)
			}
			var marks, drops, responses uint64
			world.VisitLinks(func(link LinkObservation) {
				marks += link.Stats.ECNMarkedPackets
				drops += link.Stats.QueueDroppedPackets
			})
			world.VisitClients(func(client ClientObservation) {
				responses += client.State.ECN.ECNResponseCount
				if !client.State.ECN.Negotiated || client.State.Transfer.Status != model.TransferSucceeded {
					t.Fatalf("client did not negotiate and complete: %+v", client.State)
				}
			})
			if marks == 0 || responses == 0 {
				t.Fatalf("marks=%d window reductions=%d queue drops=%d", marks, responses, drops)
			}
		})
	}
}

// A mark belongs to the packet copy crossing the queue, not the sender's original.
// A later marked hop preserves CE without counting a second local transition.
func TestECNMetadataSurvivesHops(t *testing.T) {
	world := New(normalizedRuntimeScenario(t, baseScenario()))
	config := scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000, QueueCapacity: 2,
		QueuePolicy: "threshold", CongestionThresholdPercent: 0, CongestionSignalProbability: 1,
	}
	first := world.addLink("test/first", "a", "b", config)
	second := world.addLink("test/second", "b", "c", config)
	// Occupy both transmitters so the test packet must join each waiting FIFO.
	first.Transmit(1, func() {})
	second.Transmit(100, func() {})
	original := model.Packet{Size: 1, ECN: model.ECN{Codepoint: model.ECT1}}
	var received model.Packet
	transmitPacket(first, original, func(packet model.Packet) {
		transmitPacket(second, packet, func(packet model.Packet) { received = packet })
	})
	if err := world.Advance(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	if received.ECN.Codepoint != model.CE || original.ECN.Codepoint != model.ECT1 ||
		first.Stats().ECNMarkedPackets != 1 || second.Stats().ECNMarkedPackets != 0 {
		t.Fatalf("original=%+v received=%+v first=%+v second=%+v", original.ECN, received.ECN, first.Stats(), second.Stats())
	}
}
