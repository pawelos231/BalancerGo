package main

import (
	"load-balancer/model"
	"net"
	"strconv"
	"time"
)

// topology projects the existing model on its owner goroutine. It uses the
// already sampled backend DTOs and copies each link's own configuration and
// counters; it neither schedules events nor exposes live model references.
func (s *simulation) topology(state model.LoadBalancerState) model.Topology {
	routedConnections := 0
	for _, backend := range state.Servers {
		routedConnections += len(backend.Clients)
	}
	graph := model.Topology{
		Nodes: make([]model.TopologyNode, 0, len(state.Servers)+2),
		Links: make([]model.TopologyLink, 0, len(s.links)),
	}
	graph.Nodes = append(graph.Nodes,
		model.TopologyNode{
			ID: "clients", Kind: model.TopologyNodeClientGroup, Label: "Clients",
			Description: "Clients share one outgoing and one incoming link to the load balancer.",
			Metrics: []model.TopologyMetric{
				{Key: "scheduledClients", Label: "Scheduled clients", Value: float64(s.scheduledClients), Unit: "clients"},
				{Key: "createdClients", Label: "Created clients", Value: float64(s.createdClients), Unit: "clients"},
				{Key: "activeClients", Label: "Active clients", Value: float64(s.createdClients - s.closedClients), Unit: "clients"},
				{Key: "completedStreams", Label: "Completed streams", Value: float64(s.completedStreams), Unit: "streams"},
			},
		},
		model.TopologyNode{
			ID: "lb", Kind: model.TopologyNodeLoadBalancer, Label: "Load balancer",
			Description: "Assigns backend servers to client connections and forwards packets.",
			Metrics: []model.TopologyMetric{
				{Key: "backendCount", Label: "Backends", Value: float64(len(state.Servers)), Unit: "backends"},
				{Key: "routedConnections", Label: "Routed connections", Value: float64(routedConnections), Unit: "connections"},
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
			ID: server.ID, Kind: model.TopologyNodeBackend, Label: label,
			Description: "Receives packets for connections assigned by the load balancer.",
			Address:     address, Active: &active,
			Metrics: []model.TopologyMetric{
				{Key: "activeConnections", Label: "Active connections", Value: float64(server.ActiveConnections), Unit: "connections"},
				{Key: "halfOpenConnections", Label: "Half-open connections", Value: float64(server.HalfOpenConnections), Unit: "connections"},
				{Key: "receivedPackets", Label: "Packets received", Value: float64(server.NumberOfHandledPackets), Unit: "packets"},
			},
		})
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
		stats := link.Link.Stats()
		graph.Links = append(graph.Links, model.TopologyLink{
			ID: link.ID, Label: link.Name, Source: link.Source, Target: link.Target,
			Config: model.TopologyLinkConfig{
				BandwidthBitsPerSecond: config.BandwidthBitsPerSecond,
				PacketTravelTimeMs:     float64(config.PacketTravelTime) / float64(time.Millisecond),
				QueueCapacity:          config.QueueCapacity,
				LossRate:               config.LossRate,
			},
			Stats: model.TopologyLinkStats{
				QueuedPackets: stats.QueuedPackets, Transmitting: stats.Transmitting,
				TravellingPackets: stats.TravellingPackets, OfferedPackets: stats.OfferedPackets,
				DeliveredPackets: stats.DeliveredPackets, QueueDroppedPackets: stats.QueueDroppedPackets,
				LostPackets: stats.LostPackets, DeliveredBytes: stats.DeliveredBytes,
			},
		})
	}
	return graph
}
