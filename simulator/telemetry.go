package main

import (
	"fmt"
	"strconv"
	"time"

	"load-balancer/model"
)

// telemetrySample separates reusable definitions from one full set of live values.
type telemetrySample struct {
	Topology model.TelemetryTopology
	Servers  []model.TelemetryServer
	Document *model.TopologyDocument
}

// telemetryPublisher belongs to the simulation owner and caches one run's definitions.
// Published documents are never changed when a later revision is built.
type telemetryPublisher struct {
	runID     string
	revision  uint64
	document  *model.TopologyDocument
	nodeIndex map[string]int
	clientIDs map[model.ClientKey]string
}

// newTelemetryPublisher starts a new run's independent revision sequence.
func newTelemetryPublisher(runID string) *telemetryPublisher {
	return &telemetryPublisher{runID: runID}
}

// sample reads owner-held models without scheduling events or advancing rate windows.
func (publisher *telemetryPublisher) sample(s *simulation, state model.LoadBalancerState) (telemetrySample, error) {
	var changed *model.TopologyDocument
	if !publisher.matches(s, state) {
		if publisher.revision == ^uint64(0) {
			return telemetrySample{}, fmt.Errorf("telemetry: topology revision limit reached")
		}
		publisher.rebuild(s, state)
		changed = publisher.document
	}

	servers, err := publisher.serverValues(state)
	if err != nil {
		return telemetrySample{}, err
	}
	return telemetrySample{
		Topology: publisher.topologyValues(s, state),
		Servers:  servers,
		Document: changed,
	}, nil
}

// matches checks membership without rebuilding labels, client keys or link settings.
func (publisher *telemetryPublisher) matches(s *simulation, state model.LoadBalancerState) bool {
	document := publisher.document
	if document == nil || len(document.Servers) != len(state.Servers) {
		return false
	}
	if len(document.Nodes) != 1+len(state.Servers)+len(s.clients) {
		return false
	}
	for index, backend := range state.Servers {
		if document.Servers[index].ID != backend.Server.ID {
			return false
		}
	}
	for _, runtime := range s.clients {
		index, exists := publisher.nodeIndex[runtime.ID]
		if !exists || document.Nodes[index].ClientKey == nil {
			return false
		}
		if *document.Nodes[index].ClientKey != runtime.Client.Key() {
			return false
		}
	}

	visibleLinks := 0
	for _, link := range s.links {
		if !publisher.visibleLink(link) {
			continue
		}
		if visibleLinks >= len(document.Links) {
			return false
		}
		previous := document.Links[visibleLinks]
		if previous.ID != link.ID || previous.Source != link.Source || previous.Target != link.Target {
			return false
		}
		visibleLinks++
	}
	return visibleLinks == len(document.Links)
}

// visibleLink excludes historical paths whose backend has been removed.
func (publisher *telemetryPublisher) visibleLink(link namedLink) bool {
	_, sourceExists := publisher.nodeIndex[link.Source]
	_, targetExists := publisher.nodeIndex[link.Target]
	return sourceExists && targetExists
}

// rebuild uses the existing projection only when definitions need a new revision.
func (publisher *telemetryPublisher) rebuild(s *simulation, state model.LoadBalancerState) {
	graph := s.topology(state)
	publisher.revision++
	document := &model.TopologyDocument{
		RunID:    publisher.runID,
		Revision: strconv.FormatUint(publisher.revision, 10),
		Nodes:    make([]model.TopologyNodeDefinition, 0, len(graph.Nodes)),
		Links:    make([]model.TopologyLinkDefinition, 0, len(graph.Links)),
		Servers:  make([]model.ServerDefinition, 0, len(state.Servers)),
	}
	nodeIndex := make(map[string]int, len(graph.Nodes))
	for index, node := range graph.Nodes {
		metrics := make([]model.TopologyMetricDefinition, 0, len(node.Metrics))
		for _, metric := range node.Metrics {
			definition := model.TopologyMetricDefinition{
				Key: metric.Key, Label: metric.Label, Unit: metric.Unit,
			}
			if staticTelemetryMetric(node.Kind, metric.Key) {
				value := metric.Value
				definition.Value = &value
			}
			metrics = append(metrics, definition)
		}
		document.Nodes = append(document.Nodes, model.TopologyNodeDefinition{
			ID:          node.ID,
			Kind:        node.Kind,
			Label:       node.Label,
			ResourceID:  node.ResourceID,
			GroupID:     node.GroupID,
			Description: node.Description,
			Address:     node.Address,
			Metrics:     metrics,
		})
		nodeIndex[node.ID] = index
	}
	clientIDs := make(map[model.ClientKey]string, len(s.clients))
	for _, runtime := range s.clients {
		key := runtime.Client.Key()
		document.Nodes[nodeIndex[runtime.ID]].ClientKey = &key
		clientIDs[key] = runtime.ID
	}
	for _, link := range graph.Links {
		document.Links = append(document.Links, model.TopologyLinkDefinition{
			ID: link.ID, Label: link.Label, Source: link.Source, Target: link.Target, Config: link.Config,
		})
	}
	if graph.Ingress != nil {
		ingress := &model.TopologyIngressDefinition{
			Config: graph.Ingress.Config,
			Queues: make([]model.TopologyInputQueueDefinition, 0, len(graph.Ingress.Queues)),
		}
		for _, queue := range graph.Ingress.Queues {
			ingress.Queues = append(ingress.Queues, model.TopologyInputQueueDefinition{
				ID: queue.ID, Side: queue.Side, Label: queue.Label, Capacity: queue.Capacity,
			})
		}
		document.Ingress = ingress
	}
	for _, backend := range state.Servers {
		server := backend.Server
		document.Servers = append(document.Servers, model.ServerDefinition{
			ID: server.ID, Name: server.Name, Address: server.Address, Port: server.Port,
		})
	}
	publisher.document = document
	publisher.nodeIndex = nodeIndex
	publisher.clientIDs = clientIDs
}

// staticTelemetryMetric keeps constants out of repeated WebSocket frames.
func staticTelemetryMetric(kind, key string) bool {
	switch kind {
	case model.TopologyNodeClient:
		return key == "payloadBytes" || key == "packetGapMs"
	case model.TopologyNodeLoadBalancer:
		return key == "ingressBitsPerSecond" || key == "forwardingBitsPerSecond" ||
			key == "backendCount" || key == "createdClients"
	default:
		return false
	}
}

// topologyValues sends IDs and live values while reusing the cached definition order.
func (publisher *telemetryPublisher) topologyValues(s *simulation, state model.LoadBalancerState) model.TelemetryTopology {
	forwarding := s.egress.Stats()
	ingress := s.ingress.Stats()
	routed := 0
	for _, backend := range state.Servers {
		routed += len(backend.Clients)
	}
	topology := model.TelemetryTopology{
		Revision: publisher.document.Revision,
		Nodes:    make([]model.TelemetryNode, 0, len(publisher.document.Nodes)),
		Links:    make([]model.TelemetryLink, 0, len(publisher.document.Links)),
		Ingress: &model.TelemetryIngress{
			Stats: topologyStats(ingress),
			Queues: []model.TelemetryInputQueue{
				{ID: "ingress/clients", Stats: topologyStats(s.clientIngress.Stats())},
				{ID: "ingress/backends", Stats: topologyStats(s.backendIngress.Stats())},
			},
		},
	}
	topology.Nodes = append(topology.Nodes, model.TelemetryNode{
		ID: loadBalancerNodeID,
		Metrics: map[string]float64{
			"ingressDeliveredPackets":       float64(ingress.DeliveredPackets),
			"ingressDeliveredBytes":         float64(ingress.DeliveredBytes),
			"routedConnections":             float64(routed),
			"forwardedPackets":              float64(forwarding.ForwardedPackets),
			"forwardedBytes":                float64(forwarding.ForwardedBytes),
			"forwardingQueuedPackets":       float64(forwarding.QueuedPackets),
			"forwardingQueueDroppedPackets": float64(forwarding.QueueDroppedPackets),
			"forwardingQueueDroppedBytes":   float64(forwarding.QueueDroppedBytes),
			"forwardingBudgetWaitMs":        float64(forwarding.BudgetWait) / float64(time.Millisecond),
			"scheduledClients":              float64(s.scheduledClients),
			"activeClients":                 float64(s.createdClients - s.closedClients),
			"completedStreams":              float64(s.completedStreams),
		},
		Traffic: s.nodeTraffic(loadBalancerNodeID),
	})
	for _, backend := range state.Servers {
		server := backend.Server
		active := server.Active
		id := backendNodeID(server.ID)
		topology.Nodes = append(topology.Nodes, model.TelemetryNode{
			ID:     id,
			Active: &active,
			Metrics: map[string]float64{
				"activeConnections":   float64(server.ActiveConnections),
				"halfOpenConnections": float64(server.HalfOpenConnections),
				"receivedPackets":     float64(server.NumberOfHandledPackets),
			},
			Traffic: s.nodeTraffic(id),
		})
	}
	for _, runtime := range s.clients {
		client := runtime.Client
		clientState := client.GetState()
		active := !client.IsClosed()
		completed := 0.0
		if client.StreamCompleted() {
			completed = 1
		}
		topology.Nodes = append(topology.Nodes, model.TelemetryNode{
			ID:     runtime.ID,
			Active: &active,
			Metrics: map[string]float64{
				"completedStreams": completed,
				"synRetries":       float64(clientState.RetrySyn),
				"dataRetries":      float64(clientState.RetryData),
			},
			Traffic: s.nodeTraffic(runtime.ID),
		})
	}
	for _, link := range s.links {
		if publisher.visibleLink(link) {
			topology.Links = append(topology.Links, model.TelemetryLink{
				ID: link.ID, Stats: topologyStats(link.Stats()),
			})
		}
	}
	return topology
}

// serverValues replaces repeated client keys with their defined topology IDs.
func (publisher *telemetryPublisher) serverValues(state model.LoadBalancerState) ([]model.TelemetryServer, error) {
	servers := make([]model.TelemetryServer, 0, len(state.Servers))
	for _, backend := range state.Servers {
		server := backend.Server
		clients := make([]model.TelemetryClient, 0, len(backend.Clients))
		for _, client := range backend.Clients {
			id, exists := publisher.clientIDs[client.Key]
			if !exists {
				return nil, fmt.Errorf("telemetry: routed client has no topology definition")
			}
			clients = append(clients, model.TelemetryClient{
				ID: id, RetrySyn: client.RetrySyn, RetryData: client.RetryData,
			})
		}
		servers = append(servers, model.TelemetryServer{
			ID:                     server.ID,
			Active:                 server.Active,
			ActiveConnections:      server.ActiveConnections,
			HalfOpenConnections:    server.HalfOpenConnections,
			LatencyP95Milli:        server.LatencyP95Milli,
			NumberOfHandledPackets: server.NumberOfHandledPackets,
			CpuUsage:               server.CpuUsage,
			Clients:                clients,
		})
	}
	return servers, nil
}
