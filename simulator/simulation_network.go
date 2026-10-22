package main

import (
	"encoding/binary"
	"hash/fnv"
	"math/rand"

	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
	"load-balancer/server"
)

// resourceRNG gives each resource its own repeatable random sequence.
// Creating other links or reading snapshots cannot change this sequence.
func (s *simulation) resourceRNG(id string) *rand.Rand {
	var seed [8]byte
	binary.LittleEndian.PutUint64(seed[:], uint64(s.scenario.Execution.Seed))

	hash := fnv.New64a()
	_, _ = hash.Write(seed[:])
	_, _ = hash.Write([]byte(id))
	return rand.New(rand.NewSource(int64(hash.Sum64())))
}

// addLink creates one directed path with its own settings, FIFO and RNG.
func (s *simulation) addLink(
	id, name, source, target string,
	config scenario.LinkConfig,
) *network.Link {
	link := network.NewLink(s.engine, configuredLink(config), s.resourceRNG(id))

	s.links = append(s.links, namedLink{
		ID:     id,
		Name:   name,
		Source: source,
		Target: target,
		Link:   link,
	})
	return link
}

// configuredLink copies the scenario's link settings into the network model.
func configuredLink(config scenario.LinkConfig) network.Config {
	return network.Config{
		BandwidthBitsPerSecond: config.BandwidthBitsPerSecond,
		PacketTravelTime:       config.PacketTravelTime,
		QueueCapacity:          config.QueueCapacity,
		LossRate:               config.LossRate,
	}
}

// connectIngress creates two waiting queues served by one shared input link.
func (s *simulation) connectIngress(config scenario.LinkConfig) {
	link := network.NewLink(
		s.engine,
		configuredLink(config),
		s.resourceRNG("link/infrastructure/ingress"),
	)
	s.ingress = network.NewIngress(link)
	s.clientIngress = s.ingress.AddInput()
	s.backendIngress = s.ingress.AddInput()
}

// addOutput registers one directed path in the shared LB dispatcher.
// Its configured queue belongs to Output; the physical link has no second FIFO.
func (s *simulation) addOutput(
	id, name, source, target string,
	config scenario.LinkConfig,
) *network.Output {
	link := s.addLink(id, name, source, target, config)
	output := s.egress.AddOutput(link)
	s.links[len(s.links)-1].Output = output
	return output
}

// addBackend connects one configured backend to the shared input and dispatcher.
func (s *simulation) addBackend(config scenario.NormalizedBackend) {
	srv := server.NewServer(
		s.engine,
		config.ID,
		config.Name,
		config.Address,
		config.Port,
		0,   // Distance.
		1,   // Initial weight.
		100, // Half-open connection limit.
	)
	endpointID := backendNodeID(config.ID)

	toBackend := s.addOutput(
		"link/"+endpointID+"/from-lb",
		"LB -> "+config.ID,
		loadBalancerNodeID,
		endpointID,
		config.Network.FromLoadBalancer,
	)
	toInput := s.addLink(
		"link/"+endpointID+"/to-lb",
		config.ID+" -> LB input",
		endpointID,
		loadBalancerNodeID,
		config.Network.ToLoadBalancer,
	)

	// A backend reply crosses its return link and then the common LB input.
	srv.SetSender(func(pkt model.Packet) {
		originalClient := s.lb.GetClient(pkt.Key)
		toInput.Transmit(pkt.WireSize(), func() {
			s.backendIngress.Transmit(pkt.WireSize(), func() {
				if s.lb.HasClient(originalClient) {
					s.lb.HandleServerPacket(srv, pkt)
				}
			})
		})
	})

	// Keep the original client identity while the packet waits or travels.
	err := s.lb.AddServer(srv, func(pkt model.Packet) {
		originalClient := s.lb.GetClient(pkt.Key)
		toBackend.Transmit(pkt.WireSize(), func() {
			if s.lb.HasClient(originalClient) {
				srv.HandlePacket(pkt)
			}
		})
	})

	if err != nil {
		panic(err)
	}

	srv.Start()
}
