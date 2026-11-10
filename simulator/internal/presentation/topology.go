// Package presentation converts synchronous world observations to detached public
// documents. It owns display labels, units and reporting aggregates.
package presentation

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/model"
	"load-balancer/network"
)

// nodeMetric is the single source for a metric's description, classification and
// current value. Definitions and live frames are projected from the same list.
type nodeMetric struct {
	key, label, unit string
	value            json.Number
	static           bool
}

// integerMetric keeps all digits, e.g. 9007199254740993 wire bytes.
func integerMetric[T ~int | ~int64 | ~uint8 | ~uint32 | ~uint64](value T) json.Number {
	if value < 0 {
		return json.Number(strconv.FormatInt(int64(value), 10))
	}
	return json.Number(strconv.FormatUint(uint64(value), 10))
}

// decimalMetric is for measured rates, ratios and durations expressed in seconds/ms.
func decimalMetric(value float64) json.Number {
	return json.Number(strconv.FormatFloat(value, 'g', -1, 64))
}

func linkLabel(link simulation.LinkObservation) string {
	if link.Source == simulation.LoadBalancerNodeID {
		return "LB -> " + strings.TrimPrefix(link.Target, "backend/")
	}
	return strings.TrimPrefix(link.Source, "backend/") + " -> LB input"
}

func ingressLabel(side string) string { return "From " + side }

func loadBalancerMetrics(info simulation.Info, ingress simulation.IngressObservation, backends, routed int, transfers contract.TransferSummary) []nodeMetric {
	forwarding := info.Forwarding
	metrics := []nodeMetric{
		{"ingressBitsPerSecond", "Shared physical input bandwidth", "bit/s", integerMetric(ingress.Config.BandwidthBitsPerSecond), true},
		{"ingressDeliveredPackets", "Packets delivered to LB", "packets", integerMetric(ingress.Stats.DeliveredPackets), false},
		{"ingressDeliveredBytes", "Wire bytes delivered to LB", "bytes", integerMetric(ingress.Stats.DeliveredBytes), false},
		{"backendCount", "Backends", "backends", integerMetric(backends), true},
		{"routedConnections", "Routed connections", "connections", integerMetric(routed), false},
		{"forwardingBitsPerSecond", "Shared forwarding limit", "bit/s", integerMetric(forwarding.ForwardingBitsPerSecond), true},
		{"forwardedPackets", "Packets started", "packets", integerMetric(forwarding.ForwardedPackets), false},
		{"forwardedBytes", "Wire bytes started", "bytes", integerMetric(forwarding.ForwardedBytes), false},
		{"forwardingQueuedPackets", "Packets waiting across LB outputs", "packets", integerMetric(forwarding.QueuedPackets), false},
		{"forwardingQueueDroppedPackets", "LB output queue drops", "packets", integerMetric(forwarding.QueueDroppedPackets), false},
		{"forwardingQueueDroppedBytes", "Bytes dropped by LB output queues", "bytes", integerMetric(forwarding.QueueDroppedBytes), false},
		{"forwardingBudgetWaitMs", "Time waiting for the shared budget", "ms", decimalMetric(float64(forwarding.BudgetWait) / float64(time.Millisecond)), false},
		{"scheduledClients", "Scheduled clients", "clients", integerMetric(info.ScheduledClients), false},
		{"createdClients", "Created clients", "clients", integerMetric(info.CreatedClients), true},
		{"activeClients", "Active clients", "clients", integerMetric(info.CreatedClients - info.ClosedClients), false},
		{"completedStreams", "Completed streams", "streams", integerMetric(info.CompletedStreams), false},
		{"receivedPayloadBytes", "Unique payload received by backends", "bytes", integerMetric(info.Payload.ReceivedBytes), false},
		{"acknowledgedPayloadBytes", "Payload confirmed to clients by ACK", "bytes", integerMetric(info.Payload.AcknowledgedBytes), false},
		{"successfulTransfers", "Successful transfers", "transfers", integerMetric(transfers.Succeeded), false},
		{"failedTransfers", "Failed transfers", "transfers", integerMetric(transfers.Failed), false},
		{"inProgressTransfers", "Transfers still in progress", "transfers", integerMetric(transfers.InProgress), false},
		{"notStartedTransfers", "Scheduled clients not yet started", "clients", integerMetric(transfers.NotStarted), false},
		{"synRetransmissions", "Cumulative SYN retransmissions", "packets", integerMetric(transfers.SynRetransmissions), false},
		{"dataRetransmissions", "Cumulative data retransmissions", "packets", integerMetric(transfers.DataRetransmissions), false},
	}
	return appendHostMetrics(metrics, info.LoadBalancerHost)
}

func backendMetrics(server model.ServerState) []nodeMetric {
	metrics := []nodeMetric{
		{"physicalAvailable", "Physically available (1 = yes)", "", boolMetric(server.Active), false},
		{"administrativeDrain", "Administrative drain (1 = yes)", "", boolMetric(server.Draining), false},
		{"observedHealthy", "LB observed healthy (1 = yes)", "", boolMetric(server.HealthTCP), false},
		{"activeConnections", "Active connections", "connections", integerMetric(server.ActiveConnections), false},
		{"halfOpenConnections", "Half-open connections", "connections", integerMetric(server.HalfOpenConnections), false},
		{"receivedPackets", "Packets delivered to backend TCP", "packets", integerMetric(server.NumberOfHandledPackets), false},
		{"receivedPayloadBytes", "Unique payload received", "bytes", integerMetric(server.ReceivedPayloadBytes), false},
		{"sentACKCount", "Standalone ACKs sent", "ACKs", integerMetric(server.ACK.SentACKCount), false},
		{"timerTriggeredACKCount", "ACKs sent when the delay timer expired", "ACKs", integerMetric(server.ACK.TimerTriggeredACKCount), false},
	}
	metrics = appendHostMetrics(metrics, server.Host)
	return appendECNMetrics(metrics, server.ECN)
}

func boolMetric(value bool) json.Number {
	if value {
		return "1"
	}
	return "0"
}

func clientMetrics(client simulation.ClientObservation) []nodeMetric {
	completed := 0.0
	if client.Completed {
		completed = 1
	}
	state := client.State
	metrics := []nodeMetric{
		{"payloadBytes", "Payload per stream", "bytes", integerMetric(client.Config.Stream.Bytes), true},
		{"acknowledgedPayloadBytes", "Payload confirmed by ACK", "bytes", integerMetric(state.AcknowledgedPayloadBytes), false},
		{"completedStreams", "Completed streams", "streams", decimalMetric(completed), false},
		{"packetGapMs", "Pause before data segments", "ms", decimalMetric(float64(client.Config.Stream.PacketGap) / float64(time.Millisecond)), true},
		{"synRetries", "Current SYN retries", "retries", integerMetric(state.RetrySyn), false},
		{"dataRetries", "Current segment retries", "retries", integerMetric(state.RetryData), false},
		{"synRetransmissions", "Cumulative SYN retransmissions", "packets", integerMetric(state.Transfer.SynRetransmissions), false},
		{"dataRetransmissions", "Cumulative data retransmissions", "packets", integerMetric(state.Transfer.DataRetransmissions), false},
		{"finRetransmissions", "Cumulative FIN retransmissions", "packets", integerMetric(state.Connection.FINRetransmissions), false},
		{"closeElapsedNanoseconds", "Elapsed connection closing time", "ns", integerMetric(state.Connection.Elapsed), false},
		{"bytesInFlight", "Payload awaiting cumulative ACK", "bytes", integerMetric(state.Congestion.BytesInFlight), false},
		{"duplicateACKs", "Current duplicate ACKs", "ACKs", integerMetric(state.Congestion.DuplicateACKs), false},
		{"fastRetransmissions", "Cumulative fast retransmissions", "packets", integerMetric(state.Congestion.FastRetransmissions), false},
	}
	// Disabled congestion control has no cwnd; avoid displaying a false zero limit.
	if state.Congestion.Mode != model.CongestionNone {
		metrics = append(metrics,
			nodeMetric{
				key: "congestionWindowBytes", label: "Congestion window (cwnd)", unit: "bytes",
				value: integerMetric(state.Congestion.CongestionWindowBytes),
			},
			nodeMetric{
				key: "slowStartThresholdBytes", label: "Slow-start threshold (ssthresh)", unit: "bytes",
				value: integerMetric(state.Congestion.SlowStartThresholdBytes),
			},
		)
	}
	return appendECNMetrics(metrics, state.ECN)
}

// appendECNMetrics keeps disabled profiles uncluttered while retaining feedback totals.
func appendECNMetrics(metrics []nodeMetric, state model.ECNState) []nodeMetric {
	if !state.Enabled {
		return metrics
	}
	negotiated := 0.0
	if state.Negotiated {
		negotiated = 1
	}
	return append(metrics,
		nodeMetric{"ecnNegotiated", "ECN negotiated (1 = yes)", "", decimalMetric(negotiated), false},
		nodeMetric{"ecnReceivedCEPacketCount", "CE-marked packets received", "packets", integerMetric(state.ReceivedCEPacketCount), false},
		nodeMetric{"ecnSentECECount", "ECE feedback sent", "ACKs", integerMetric(state.SentECECount), false},
		nodeMetric{"ecnReceivedECECount", "ECE feedback received", "ACKs", integerMetric(state.ReceivedECECount), false},
		nodeMetric{"ecnSentCWRCount", "CWR signals sent", "packets", integerMetric(state.SentCWRCount), false},
		nodeMetric{"ecnResponseCount", "ECN responses", "responses", integerMetric(state.ECNResponseCount), false},
	)
}

func metricDefinitions(metrics []nodeMetric) []contract.TopologyMetricDefinition {
	definitions := make([]contract.TopologyMetricDefinition, 0, len(metrics))
	for _, metric := range metrics {
		definition := contract.TopologyMetricDefinition{Key: metric.key, Label: metric.label, Unit: metric.unit}
		if metric.static {
			value, _ := metric.value.Float64()
			definition.Value = &value
			exact := metric.value
			definition.ExactValue = &exact
		}
		definitions = append(definitions, definition)
	}
	return definitions
}

func metricValues(metrics []nodeMetric) map[string]float64 {
	values := make(map[string]float64, len(metrics))
	for _, metric := range metrics {
		if !metric.static {
			values[metric.key], _ = metric.value.Float64()
		}
	}
	return values
}

func exactMetricValues(metrics []nodeMetric) map[string]json.Number {
	values := make(map[string]json.Number, len(metrics))
	for _, metric := range metrics {
		if !metric.static {
			values[metric.key] = metric.value
		}
	}
	return values
}

func backendDefinition(server model.ServerState, metrics []nodeMetric) contract.TopologyNodeDefinition {
	label := server.Name
	if label == "" {
		label = server.ID
	}
	address := ""
	if server.Address != "" {
		address = net.JoinHostPort(server.Address, strconv.Itoa(int(server.Port)))
	}
	return contract.TopologyNodeDefinition{
		ID: simulation.BackendNodeID(server.ID), Kind: contract.TopologyNodeBackend,
		Label: label, ResourceID: server.ID,
		Description: "Receives packets for connections assigned by the load balancer.",
		Address:     address, Metrics: metricDefinitions(metrics),
	}
}

func clientDefinition(client simulation.ClientObservation, metrics []nodeMetric) contract.TopologyNodeDefinition {
	key := clientKey(client.State.Key)
	return contract.TopologyNodeDefinition{
		ID: client.ID, Kind: contract.TopologyNodeClient,
		Label: fmt.Sprintf("Client %d (%s)", client.Index+1, client.GroupID), GroupID: client.GroupID,
		Description: "One TCP client with its own links to the client side of the load balancer.",
		Address:     net.JoinHostPort(key.SrcIP, strconv.Itoa(int(key.SrcPort))),
		Metrics:     metricDefinitions(metrics), ClientKey: &key,
	}
}

func nodeTraffic(world *simulation.World, id string) *contract.TopologyTraffic {
	rate, exists := world.NodeTraffic(id)
	if !exists {
		return nil
	}
	return &contract.TopologyTraffic{
		TransmitBytesPerSecond: rate.TransmitBytesPerSecond,
		ReceiveBytesPerSecond:  rate.ReceiveBytesPerSecond,
		SampleWindowSeconds:    rate.SampleWindow.Seconds(),
	}
}

func topologyConfig(config network.Config) contract.TopologyLinkConfig {
	return contract.TopologyLinkConfig{
		BandwidthBitsPerSecond: config.BandwidthBitsPerSecond,
		PacketTravelTimeMs:     float64(config.PacketTravelTime) / float64(time.Millisecond),
		QueueCapacity:          config.QueueCapacity, LossRate: config.LossRate,
		QueuePolicy:                 config.QueuePolicy,
		CongestionThresholdPercent:  config.CongestionThresholdPercent,
		CongestionSignalProbability: config.CongestionSignalProbability,
	}
}

func topologyStats(stats network.Stats) contract.TopologyLinkStats {
	return contract.TopologyLinkStats{
		QueuedPackets: stats.QueuedPackets, Transmitting: stats.Transmitting,
		TravellingPackets: stats.TravellingPackets, OfferedPackets: stats.OfferedPackets,
		OfferedBytes: stats.OfferedBytes, AcceptedPackets: stats.AcceptedPackets,
		AcceptedBytes: stats.AcceptedBytes, DeliveredPackets: stats.DeliveredPackets,
		DeliveredBytes: stats.DeliveredBytes, QueueDroppedPackets: stats.QueueDroppedPackets,
		QueueDroppedBytes: stats.QueueDroppedBytes, LostPackets: stats.LostPackets, LostBytes: stats.LostBytes,
		ECNMarkedPackets: stats.ECNMarkedPackets, ECNMarkedBytes: stats.ECNMarkedBytes,
		CongestionDroppedPackets: stats.CongestionDroppedPackets,
		CongestionDroppedBytes:   stats.CongestionDroppedBytes,
	}
}

// BuildTopology provides the existing complete graph for callers that need it.
// Runtime publication uses Sample directly and never builds this full graph.
func BuildTopology(world *simulation.World) contract.Topology {
	sample, err := NewProjector("").Sample(world)
	if err != nil {
		panic(err) // A native world's routes always refer to its retained clients.
	}
	document := sample.Document
	graph := contract.Topology{
		Nodes: make([]contract.TopologyNode, 0, len(document.Nodes)),
		Links: make([]contract.TopologyLink, 0, len(document.Links)),
	}
	for index, definition := range document.Nodes {
		live := sample.Topology.Nodes[index]
		metrics := make([]contract.TopologyMetric, 0, len(definition.Metrics))
		for _, metric := range definition.Metrics {
			value := live.Metrics[metric.Key]
			if metric.Value != nil {
				value = *metric.Value
			}
			metrics = append(metrics, contract.TopologyMetric{
				Key: metric.Key, Label: metric.Label, Unit: metric.Unit, Value: value,
			})
		}
		graph.Nodes = append(graph.Nodes, contract.TopologyNode{
			ID: definition.ID, Kind: definition.Kind, Label: definition.Label, ResourceID: definition.ResourceID,
			GroupID: definition.GroupID, Description: definition.Description, Address: definition.Address,
			Active: live.Active, Metrics: metrics, Traffic: live.Traffic,
			TCPPhase: live.TCPPhase,
			TCPState: live.TCPState, CloseOutcome: live.CloseOutcome,
		})
	}
	for index, definition := range document.Links {
		graph.Links = append(graph.Links, contract.TopologyLink{
			ID: definition.ID, Label: definition.Label, Source: definition.Source, Target: definition.Target,
			Config: definition.Config, Stats: sample.Topology.Links[index].Stats,
		})
	}
	graph.Ingress = &contract.TopologyIngress{
		Config: document.Ingress.Config, Stats: sample.Topology.Ingress.Stats,
		Queues: make([]contract.TopologyInputQueue, 0, len(document.Ingress.Queues)),
	}
	for index, queue := range document.Ingress.Queues {
		graph.Ingress.Queues = append(graph.Ingress.Queues, contract.TopologyInputQueue{
			ID: queue.ID, Side: queue.Side, Label: queue.Label, Capacity: queue.Capacity,
			Stats: sample.Topology.Ingress.Queues[index].Stats,
		})
	}
	return graph
}
