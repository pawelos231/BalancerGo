package main

import (
	"fmt"
	"load-balancer/model"
	"net"
	"strconv"
	"time"
)

// Graph namespaces keep scenario resource IDs separate from infrastructure IDs.
const (
	loadBalancerNodeID = "infrastructure/lb"
	upstreamNodeID     = "infrastructure/upstream"
)

// backendNodeID keeps native backend IDs separate from infrastructure and clients.
func backendNodeID(resourceID string) string {
	return "backend/" + resourceID
}

// topology projects the existing model on its owner goroutine. It uses the
// already sampled backend DTOs and copies each link's own configuration and
// counters; it neither schedules events nor exposes live model references.
func (s *simulation) topology(state model.LoadBalancerState) model.Topology {
	forwarding := s.egress.Stats()
	ingress := s.ingress.Stats()
	routedConnections := 0
	for _, backend := range state.Servers {
		routedConnections += len(backend.Clients)
	}
	graph := model.Topology{
		Nodes: make([]model.TopologyNode, 0, len(state.Servers)+len(s.clients)+2),
		Links: make([]model.TopologyLink, 0, len(s.links)),
	}
	graph.Nodes = append(graph.Nodes,
		model.TopologyNode{
			ID:          upstreamNodeID,
			Kind:        model.TopologyNodeNetworkAggregator,
			Label:       "Upstream aggregation",
			Description: "Client and backend return traffic joins the FIFO before the shared physical LB input. This is an upstream network buffer.",
			Metrics: []model.TopologyMetric{
				{
					Key:   "queuedPackets",
					Label: "Packets waiting before LB",
					Value: float64(ingress.QueuedPackets),
					Unit:  "packets",
				},
				{
					Key:   "queueDroppedPackets",
					Label: "Queue drops before LB",
					Value: float64(ingress.QueueDroppedPackets),
					Unit:  "packets",
				},
				{
					Key:   "queueDroppedBytes",
					Label: "Bytes dropped before LB",
					Value: float64(ingress.QueueDroppedBytes),
					Unit:  "bytes",
				},
			},
		},
		model.TopologyNode{
			ID:          loadBalancerNodeID,
			Kind:        model.TopologyNodeLoadBalancer,
			Label:       "Load balancer",
			ResourceID:  s.scenario.LoadBalancer.ID,
			Description: "Assigns backends and shares one forwarding budget across packets sent to clients and backends.",
			Metrics: []model.TopologyMetric{
				{
					Key:   "ingressBitsPerSecond",
					Label: "Shared physical input bandwidth",
					Value: float64(s.ingress.Config().BandwidthBitsPerSecond),
					Unit:  "bit/s",
				},
				{
					Key:   "ingressDeliveredPackets",
					Label: "Packets delivered to LB",
					Value: float64(ingress.DeliveredPackets),
					Unit:  "packets",
				},
				{
					Key:   "ingressDeliveredBytes",
					Label: "Wire bytes delivered to LB",
					Value: float64(ingress.DeliveredBytes),
					Unit:  "bytes",
				},
				{
					Key:   "backendCount",
					Label: "Backends",
					Value: float64(len(state.Servers)),
					Unit:  "backends",
				},
				{
					Key:   "routedConnections",
					Label: "Routed connections",
					Value: float64(routedConnections),
					Unit:  "connections",
				},
				{
					Key:   "forwardingBitsPerSecond",
					Label: "Shared forwarding limit",
					Value: float64(forwarding.ForwardingBitsPerSecond),
					Unit:  "bit/s",
				},
				{
					Key:   "forwardedPackets",
					Label: "Packets started",
					Value: float64(forwarding.ForwardedPackets),
					Unit:  "packets",
				},
				{
					Key:   "forwardedBytes",
					Label: "Wire bytes started",
					Value: float64(forwarding.ForwardedBytes),
					Unit:  "bytes",
				},
				{
					Key:   "forwardingQueuedPackets",
					Label: "Packets waiting across LB outputs",
					Value: float64(forwarding.QueuedPackets),
					Unit:  "packets",
				},
				{
					Key:   "forwardingQueueDroppedPackets",
					Label: "LB output queue drops",
					Value: float64(forwarding.QueueDroppedPackets),
					Unit:  "packets",
				},
				{
					Key:   "forwardingQueueDroppedBytes",
					Label: "Bytes dropped by LB output queues",
					Value: float64(forwarding.QueueDroppedBytes),
					Unit:  "bytes",
				},
				{
					Key:   "forwardingBudgetWaitMs",
					Label: "Time waiting for the shared budget",
					Value: float64(forwarding.BudgetWait) / float64(time.Millisecond),
					Unit:  "ms",
				},
				{
					Key:   "scheduledClients",
					Label: "Scheduled clients",
					Value: float64(s.scheduledClients),
					Unit:  "clients",
				},
				{
					Key:   "createdClients",
					Label: "Created clients",
					Value: float64(s.createdClients),
					Unit:  "clients",
				},
				{
					Key:   "activeClients",
					Label: "Active clients",
					Value: float64(s.createdClients - s.closedClients),
					Unit:  "clients",
				},
				{
					Key:   "completedStreams",
					Label: "Completed streams",
					Value: float64(s.completedStreams),
					Unit:  "streams",
				},
			},
		},
	)
	for _, backend := range state.Servers {
		server := backend.Server
		label := server.Name
		if label == "" {
			label = server.ID
		}
		address := ""
		if server.Address != "" {
			address = net.JoinHostPort(server.Address, strconv.Itoa(int(server.Port)))
		}
		active := server.Active
		graph.Nodes = append(graph.Nodes, model.TopologyNode{
			ID:          backendNodeID(server.ID),
			Kind:        model.TopologyNodeBackend,
			Label:       label,
			ResourceID:  server.ID,
			Description: "Receives packets for connections assigned by the load balancer.",
			Address:     address,
			Active:      &active,
			Metrics: []model.TopologyMetric{
				{
					Key:   "activeConnections",
					Label: "Active connections",
					Value: float64(server.ActiveConnections),
					Unit:  "connections",
				},
				{
					Key:   "halfOpenConnections",
					Label: "Half-open connections",
					Value: float64(server.HalfOpenConnections),
					Unit:  "connections",
				},
				{
					Key:   "receivedPackets",
					Label: "Packets received",
					Value: float64(server.NumberOfHandledPackets),
					Unit:  "packets",
				},
			},
		})
	}
	for _, runtime := range s.clients {
		graph.Nodes = append(graph.Nodes, clientTopologyNode(runtime))
	}
	// Removing a backend unregisters it from the balancer while its old link
	// objects can still retain historical counters. Only show current endpoints.
	nodeIDs := make(map[string]bool, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodeIDs[node.ID] = true
	}
	for _, link := range s.links {
		if !nodeIDs[link.Source] || !nodeIDs[link.Target] {
			continue
		}
		config := link.Link.Config()
		stats := link.Stats()
		graph.Links = append(graph.Links, model.TopologyLink{
			ID:     link.ID,
			Label:  link.Name,
			Source: link.Source,
			Target: link.Target,
			Config: model.TopologyLinkConfig{
				BandwidthBitsPerSecond: config.BandwidthBitsPerSecond,
				PacketTravelTimeMs:     float64(config.PacketTravelTime) / float64(time.Millisecond),
				QueueCapacity:          config.QueueCapacity,
				LossRate:               config.LossRate,
			},
			Stats: model.TopologyLinkStats{
				QueuedPackets:       stats.QueuedPackets,
				Transmitting:        stats.Transmitting,
				TravellingPackets:   stats.TravellingPackets,
				OfferedPackets:      stats.OfferedPackets,
				OfferedBytes:        stats.OfferedBytes,
				AcceptedPackets:     stats.AcceptedPackets,
				AcceptedBytes:       stats.AcceptedBytes,
				DeliveredPackets:    stats.DeliveredPackets,
				DeliveredBytes:      stats.DeliveredBytes,
				QueueDroppedPackets: stats.QueueDroppedPackets,
				QueueDroppedBytes:   stats.QueueDroppedBytes,
				LostPackets:         stats.LostPackets,
				LostBytes:           stats.LostBytes,
			},
		})
	}
	return graph
}

// clientTopologyNode copies one created endpoint's identity, stream and live state.
// Closed clients remain visible with the historical counters on their own links.
func clientTopologyNode(runtime runtimeClient) model.TopologyNode {
	state := runtime.Client.GetState()
	active := !runtime.Client.IsClosed()
	completedStreams := 0.0
	if runtime.Client.StreamCompleted() {
		completedStreams = 1
	}
	return model.TopologyNode{
		ID:          runtime.ID,
		Kind:        model.TopologyNodeClient,
		Label:       fmt.Sprintf("Client %d (%s)", runtime.Index+1, runtime.GroupID),
		GroupID:     runtime.GroupID,
		Description: "One created TCP client with its own links to upstream and from the load balancer.",
		Address:     net.JoinHostPort(state.Key.SrcIP, strconv.Itoa(int(state.Key.SrcPort))),
		Active:      &active,
		Metrics: []model.TopologyMetric{
			{
				Key:   "payloadBytes",
				Label: "Payload per stream",
				Value: float64(runtime.Config.Stream.Bytes),
				Unit:  "bytes",
			},
			{
				Key:   "completedStreams",
				Label: "Completed streams",
				Value: completedStreams,
				Unit:  "streams",
			},
			{
				Key:   "packetGapMs",
				Label: "Pause before data segments",
				Value: float64(runtime.Config.Stream.PacketGap) / float64(time.Millisecond),
				Unit:  "ms",
			},
			{
				Key:   "synRetries",
				Label: "Current SYN retries",
				Value: float64(state.RetrySyn),
				Unit:  "retries",
			},
			{
				Key:   "dataRetries",
				Label: "Current segment retries",
				Value: float64(state.RetryData),
				Unit:  "retries",
			},
		},
	}
}
