// Package presentation converts synchronous world observations to detached public
// documents. It owns display labels, units and reporting aggregates.
package presentation

import (
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
	value            float64
	static           bool
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
	return []nodeMetric{
		{"ingressBitsPerSecond", "Shared physical input bandwidth", "bit/s", float64(ingress.Config.BandwidthBitsPerSecond), true},
		{"ingressDeliveredPackets", "Packets delivered to LB", "packets", float64(ingress.Stats.DeliveredPackets), false},
		{"ingressDeliveredBytes", "Wire bytes delivered to LB", "bytes", float64(ingress.Stats.DeliveredBytes), false},
		{"backendCount", "Backends", "backends", float64(backends), true},
		{"routedConnections", "Routed connections", "connections", float64(routed), false},
		{"forwardingBitsPerSecond", "Shared forwarding limit", "bit/s", float64(forwarding.ForwardingBitsPerSecond), true},
		{"forwardedPackets", "Packets started", "packets", float64(forwarding.ForwardedPackets), false},
		{"forwardedBytes", "Wire bytes started", "bytes", float64(forwarding.ForwardedBytes), false},
		{"forwardingQueuedPackets", "Packets waiting across LB outputs", "packets", float64(forwarding.QueuedPackets), false},
		{"forwardingQueueDroppedPackets", "LB output queue drops", "packets", float64(forwarding.QueueDroppedPackets), false},
		{"forwardingQueueDroppedBytes", "Bytes dropped by LB output queues", "bytes", float64(forwarding.QueueDroppedBytes), false},
		{"forwardingBudgetWaitMs", "Time waiting for the shared budget", "ms", float64(forwarding.BudgetWait) / float64(time.Millisecond), false},
		{"scheduledClients", "Scheduled clients", "clients", float64(info.ScheduledClients), false},
		{"createdClients", "Created clients", "clients", float64(info.CreatedClients), true},
		{"activeClients", "Active clients", "clients", float64(info.CreatedClients - info.ClosedClients), false},
		{"completedStreams", "Completed streams", "streams", float64(info.CompletedStreams), false},
		{"receivedPayloadBytes", "Unique payload received by backends", "bytes", float64(info.Payload.ReceivedBytes), false},
		{"acknowledgedPayloadBytes", "Payload confirmed to clients by ACK", "bytes", float64(info.Payload.AcknowledgedBytes), false},
		{"successfulTransfers", "Successful transfers", "transfers", float64(transfers.Succeeded), false},
		{"failedTransfers", "Failed transfers", "transfers", float64(transfers.Failed), false},
		{"inProgressTransfers", "Transfers still in progress", "transfers", float64(transfers.InProgress), false},
		{"notStartedTransfers", "Scheduled clients not yet started", "clients", float64(transfers.NotStarted), false},
		{"synRetransmissions", "Cumulative SYN retransmissions", "packets", float64(transfers.SynRetransmissions), false},
		{"dataRetransmissions", "Cumulative data retransmissions", "packets", float64(transfers.DataRetransmissions), false},
	}
}

func backendMetrics(server model.ServerState) []nodeMetric {
	return []nodeMetric{
		{"activeConnections", "Active connections", "connections", float64(server.ActiveConnections), false},
		{"halfOpenConnections", "Half-open connections", "connections", float64(server.HalfOpenConnections), false},
		{"receivedPackets", "Packets received", "packets", float64(server.NumberOfHandledPackets), false},
		{"receivedPayloadBytes", "Unique payload received", "bytes", float64(server.ReceivedPayloadBytes), false},
	}
}

func clientMetrics(client simulation.ClientObservation) []nodeMetric {
	completed := 0.0
	if client.Completed {
		completed = 1
	}
	state := client.State
	return []nodeMetric{
		{"payloadBytes", "Payload per stream", "bytes", float64(client.Config.Stream.Bytes), true},
		{"acknowledgedPayloadBytes", "Payload confirmed by ACK", "bytes", float64(state.AcknowledgedPayloadBytes), false},
		{"completedStreams", "Completed streams", "streams", completed, false},
		{"packetGapMs", "Pause before data segments", "ms", float64(client.Config.Stream.PacketGap) / float64(time.Millisecond), true},
		{"synRetries", "Current SYN retries", "retries", float64(state.RetrySyn), false},
		{"dataRetries", "Current segment retries", "retries", float64(state.RetryData), false},
		{"synRetransmissions", "Cumulative SYN retransmissions", "packets", float64(state.Transfer.SynRetransmissions), false},
		{"dataRetransmissions", "Cumulative data retransmissions", "packets", float64(state.Transfer.DataRetransmissions), false},
	}
}

func metricDefinitions(metrics []nodeMetric) []contract.TopologyMetricDefinition {
	definitions := make([]contract.TopologyMetricDefinition, 0, len(metrics))
	for _, metric := range metrics {
		definition := contract.TopologyMetricDefinition{Key: metric.key, Label: metric.label, Unit: metric.unit}
		if metric.static {
			value := metric.value
			definition.Value = &value
		}
		definitions = append(definitions, definition)
	}
	return definitions
}

func metricValues(metrics []nodeMetric) map[string]float64 {
	values := make(map[string]float64, len(metrics))
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
			metrics = append(metrics, contract.TopologyMetric{Key: metric.Key, Label: metric.Label, Unit: metric.Unit, Value: value})
		}
		graph.Nodes = append(graph.Nodes, contract.TopologyNode{
			ID: definition.ID, Kind: definition.Kind, Label: definition.Label, ResourceID: definition.ResourceID,
			GroupID: definition.GroupID, Description: definition.Description, Address: definition.Address,
			Active: live.Active, Metrics: metrics, Traffic: live.Traffic,
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
