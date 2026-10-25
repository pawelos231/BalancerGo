package presentation

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/model"
)

// Sample separates reusable definitions from one complete set of live values.
type Sample struct {
	Topology  contract.TelemetryTopology
	Servers   []contract.TelemetryServer
	Document  *contract.TopologyDocument
	Transfers contract.TransferSummary
}

// Projector belongs to the world owner. Its previously returned definitions are
// immutable, and every live value is detached before publication can retain it.
type Projector struct {
	runID     string
	revision  uint64
	document  *contract.TopologyDocument
	nodeIndex map[string]int
}

func NewProjector(runID string) *Projector { return &Projector{runID: runID} }

// RunState supplies lifecycle and measured wall time from the application.
// Algorithm is the canonical configured name used when no world exists.
type RunState struct {
	Status, RunID, Algorithm, Error string
	Elapsed                         time.Duration
}

// BuildSnapshot owns the current browser representation while the application
// keeps lifecycle decisions, real time and publication ordering.
func BuildSnapshot(world *simulation.World, projector *Projector, state RunState) (contract.SimulationSnapshot, *contract.TopologyDocument, error) {
	snapshot := contract.SimulationSnapshot{
		Type: "simulationSnapshot", Version: 1,
		Mode: contract.LBModeNAT, Algorithm: AlgorithmLabel(state.Algorithm),
		Servers: make([]contract.TelemetryServer, 0),
		Simulation: contract.SimulationMetadata{
			Status: state.Status, RunID: state.RunID, Algorithm: state.Algorithm, Error: state.Error,
		},
		Topology: contract.TelemetryTopology{
			Revision: "0", Nodes: make([]contract.TelemetryNode, 0), Links: make([]contract.TelemetryLink, 0),
		},
	}
	if world == nil {
		return snapshot, nil, nil
	}
	sample, err := projector.Sample(world)
	if err != nil {
		return contract.SimulationSnapshot{}, nil, err
	}
	info := world.Info()
	snapshot.Mode, snapshot.Algorithm = contract.LBMode(info.Mode), AlgorithmLabel(info.Algorithm)
	snapshot.Servers, snapshot.Topology = sample.Servers, sample.Topology
	snapshot.Simulation.Algorithm = info.Algorithm
	snapshot.Simulation.VirtualTimeSeconds = info.Now.Seconds()
	snapshot.Simulation.Performance = MeasurePerformance(state.Elapsed, info.EventsExecuted, info.Forwarding.ForwardedPackets)
	snapshot.Simulation.Payload = &contract.PayloadProgress{
		ReceivedBytes: info.Payload.ReceivedBytes, AcknowledgedBytes: info.Payload.AcknowledgedBytes,
	}
	snapshot.Simulation.Transfers = &sample.Transfers
	return snapshot, sample.Document, nil
}

type routeSlot struct{ server, client int }

// Sample visits each client once to fill its node, routed-client slots and the
// transfer aggregate. Link definitions and counters likewise share one visit.
// Reading observations does not execute events or advance traffic windows.
func (projector *Projector) Sample(world *simulation.World) (Sample, error) {
	info := world.Info()
	ingress := world.Ingress()
	var document *contract.TopologyDocument
	topology := contract.TelemetryTopology{
		Nodes: make([]contract.TelemetryNode, 1),
		Links: make([]contract.TelemetryLink, 0),
		Ingress: &contract.TelemetryIngress{
			Stats:  topologyStats(ingress.Stats),
			Queues: make([]contract.TelemetryInputQueue, 0, len(ingress.Inputs)),
		},
	}
	topology.Nodes[0].ID = simulation.LoadBalancerNodeID
	for _, input := range ingress.Inputs {
		topology.Ingress.Queues = append(topology.Ingress.Queues, contract.TelemetryInputQueue{
			ID: input.ID, Stats: topologyStats(input.Stats),
		})
	}

	matches := projector.document != nil
	nodeIndex := projector.nodeIndex
	serverIndex := make(map[string]int)
	servers := make([]contract.TelemetryServer, 0)
	backends := make([]model.ServerState, 0)
	world.VisitBackends(func(backend simulation.BackendObservation) {
		server := backend.State
		index := len(servers)
		if matches && (index >= len(projector.document.Servers) || projector.document.Servers[index].ID != server.ID) {
			matches = false
		}
		serverIndex[server.ID] = index
		backends = append(backends, server)
		servers = append(servers, contract.TelemetryServer{
			ID: server.ID, Active: server.Active,
			ActiveConnections: server.ActiveConnections, HalfOpenConnections: server.HalfOpenConnections,
			LatencyP95Milli: server.LatencyP95Milli, NumberOfHandledPackets: server.NumberOfHandledPackets,
			ReceivedPayloadBytes: server.ReceivedPayloadBytes,
			CpuUsage:             float32(server.CpuUsage) / float32(legacyCPUScale),
			Clients:              make([]contract.TelemetryClient, 0),
		})
		metrics := backendMetrics(server)
		id := simulation.BackendNodeID(server.ID)
		active := server.Active
		topology.Nodes = append(topology.Nodes, contract.TelemetryNode{
			ID: id, Active: &active, Metrics: metricValues(metrics), Traffic: nodeTraffic(world, id),
		})
	})
	if matches && (len(servers) != len(projector.document.Servers) || len(projector.document.Nodes) != 1+len(servers)+info.CreatedClients) {
		matches = false
	}
	topology.Nodes = slices.Grow(topology.Nodes, info.CreatedClients)

	// Allocate definitions only after membership first differs. The already read
	// backend values refresh current metadata; unchanged client prefixes reuse
	// their immutable cached definitions without revisiting retained endpoints.
	ensureDocument := func() {
		if document != nil {
			return
		}
		document = &contract.TopologyDocument{
			RunID:   projector.runID,
			Nodes:   make([]contract.TopologyNodeDefinition, 1, 1+len(backends)+info.CreatedClients),
			Servers: make([]contract.ServerDefinition, 0, len(backends)),
		}
		nodeIndex = make(map[string]int, 1+len(backends)+info.CreatedClients)
		for index, node := range topology.Nodes {
			nodeIndex[node.ID] = index
		}
		for _, server := range backends {
			document.Nodes = append(document.Nodes, backendDefinition(server, backendMetrics(server)))
			document.Servers = append(document.Servers, contract.ServerDefinition{
				ID: server.ID, Name: server.Name, Address: server.Address, Port: server.Port,
			})
		}
		for _, node := range topology.Nodes[1+len(backends):] {
			document.Nodes = append(document.Nodes, projector.document.Nodes[projector.nodeIndex[node.ID]])
		}
	}
	if !matches {
		ensureDocument()
	}

	// Allocate slots in connection-table order before visiting clients in creation
	// order. Filling these positions preserves both independent orderings.
	slots := make(map[model.ClientKey]routeSlot)
	routed := 0
	world.VisitRoutes(func(route simulation.RouteObservation) {
		server := serverIndex[route.BackendID]
		slots[route.Key] = routeSlot{server, len(servers[server].Clients)}
		servers[server].Clients = append(servers[server].Clients, contract.TelemetryClient{})
		routed++
	})
	unresolved := routed
	transfers := newTransferSummary(info)
	world.VisitClients(func(client simulation.ClientObservation) {
		state := client.State
		addTransfer(&transfers, state.Transfer)
		metrics := clientMetrics(client)
		key := clientKey(state.Key)
		var definition contract.TopologyNodeDefinition
		if previous, exists := projector.nodeIndex[client.ID]; exists && projector.document.Nodes[previous].ClientKey != nil &&
			*projector.document.Nodes[previous].ClientKey == key {
			// A retained client's identity/configuration is immutable. Sharing its
			// immutable definition also keeps older documents safe to retain.
			definition = projector.document.Nodes[previous]
		} else {
			matches = false
			ensureDocument()
			definition = clientDefinition(client, metrics)
		}
		if document != nil {
			document.Nodes = append(document.Nodes, definition)
			nodeIndex[client.ID] = len(topology.Nodes)
		}
		active := client.Active
		topology.Nodes = append(topology.Nodes, contract.TelemetryNode{
			ID: client.ID, Active: &active, Metrics: metricValues(metrics), Traffic: nodeTraffic(world, client.ID),
		})
		if slot, routed := slots[state.Key]; routed {
			servers[slot.server].Clients[slot.client] = contract.TelemetryClient{
				ID: client.ID, RetrySyn: state.RetrySyn, RetryData: state.RetryData,
				AcknowledgedPayloadBytes: state.AcknowledgedPayloadBytes, Transfer: transferState(state.Transfer),
			}
			unresolved--
		}
	})
	metrics := loadBalancerMetrics(info, ingress, len(servers), routed, transfers)
	topology.Nodes[0] = contract.TelemetryNode{
		ID: simulation.LoadBalancerNodeID, Metrics: metricValues(metrics), Traffic: nodeTraffic(world, simulation.LoadBalancerNodeID),
	}

	ensureLinks := func(prefix int) {
		ensureDocument()
		if document.Links == nil {
			capacity := prefix
			if projector.document != nil {
				capacity = max(capacity, len(projector.document.Links))
			}
			document.Links = make([]contract.TopologyLinkDefinition, 0, capacity)
			if prefix > 0 {
				document.Links = append(document.Links, projector.document.Links[:prefix]...)
			}
		}
	}
	world.VisitLinks(func(link simulation.LinkObservation) {
		_, sourceExists := nodeIndex[link.Source]
		_, targetExists := nodeIndex[link.Target]
		if !sourceExists || !targetExists {
			return // Removed backends retain physical history, outside this graph.
		}
		index := len(topology.Links)
		if matches {
			if index >= len(projector.document.Links) {
				matches = false
			} else {
				previous := projector.document.Links[index]
				if previous.ID != link.ID || previous.Source != link.Source || previous.Target != link.Target {
					matches = false
				}
			}
		}
		if !matches {
			ensureLinks(index)
			document.Links = append(document.Links, contract.TopologyLinkDefinition{
				ID: link.ID, Label: linkLabel(link), Source: link.Source, Target: link.Target, Config: topologyConfig(link.Config),
			})
		}
		topology.Links = append(topology.Links, contract.TelemetryLink{ID: link.ID, Stats: topologyStats(link.Stats)})
	})
	if matches && len(topology.Links) != len(projector.document.Links) {
		matches = false
	}

	var changed *contract.TopologyDocument
	if !matches {
		if projector.revision == ^uint64(0) {
			return Sample{}, fmt.Errorf("telemetry: topology revision limit reached")
		}
		ensureLinks(len(topology.Links))
		document.Nodes[0] = contract.TopologyNodeDefinition{
			ID: simulation.LoadBalancerNodeID, Kind: contract.TopologyNodeLoadBalancer,
			Label: "Load balancer", ResourceID: info.LoadBalancerID,
			Description: "Client and backend input queues share one receiver. All outputs share one forwarding budget.",
			Metrics:     metricDefinitions(metrics),
		}
		document.Ingress = &contract.TopologyIngressDefinition{
			Config: topologyConfig(ingress.Config),
			Queues: make([]contract.TopologyInputQueueDefinition, 0, len(ingress.Inputs)),
		}
		for _, input := range ingress.Inputs {
			document.Ingress.Queues = append(document.Ingress.Queues, contract.TopologyInputQueueDefinition{
				ID: input.ID, Side: input.Side, Label: ingressLabel(input.Side), Capacity: input.Capacity,
			})
		}
		projector.revision++
		document.Revision = strconv.FormatUint(projector.revision, 10)
		projector.document, projector.nodeIndex = document, nodeIndex
		changed = document
	}
	if unresolved != 0 {
		return Sample{}, fmt.Errorf("telemetry: routed client has no topology definition")
	}
	topology.Revision = projector.document.Revision
	return Sample{Topology: topology, Servers: servers, Document: changed, Transfers: transfers}, nil
}
