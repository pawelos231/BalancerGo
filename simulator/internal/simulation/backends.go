package simulation

import (
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
	"load-balancer/server"
)

// A backend owns its physical paths for one lifetime. Retired paths keep their history.
type runtimeBackend struct {
	server     *server.Server
	output     *network.Output
	returnLink *network.Link
	present    bool
}

// addBackend creates resources now, so CPU observations start at insertion time.
func (s *World) addBackend(config scenario.NormalizedBackend) {
	srv := server.NewServer(
		s.engine, config.ID, config.Name, config.Address, config.Port,
		0, 1, 100, // Distance, initial weight and half-open limit.
	)
	srv.SetHostConfig(configuredHost(config.Host))
	srv.SetReceiveBufferBytes(s.scenario.TCP.ReceiveBufferBytes)
	srv.SetSACKEnabled(s.scenario.TCP.SACKEnabled)
	srv.SetECNEnabled(s.scenario.TCP.ECNEnabled)
	srv.SetDelayedACKConfig(configuredDelayedACK(s.scenario.TCP.DelayedACK))
	srv.SetRTOConfig(configuredRTO(s.scenario.TCP.RTO))
	srv.SetCloseConfig(configuredClose(s.scenario.TCP.Close))
	srv.SetIdleTimeouts(
		s.scenario.TCP.BackendHalfOpenIdleTimeout,
		s.scenario.TCP.BackendEstablishedIdleTimeout,
	)
	srv.OnPayloadReceived = func(bytes int) {
		s.payload.ReceivedBytes += uint64(bytes)
	}

	endpointID := BackendNodeID(config.ID)
	backend := &runtimeBackend{
		server: srv,
		output: s.addOutput(
			"link/"+endpointID+"/from-lb", LoadBalancerNodeID, endpointID,
			config.Network.FromLoadBalancer,
		),
		returnLink: s.addLink(
			"link/"+endpointID+"/to-lb", endpointID, LoadBalancerNodeID,
			config.Network.ToLoadBalancer,
		),
		present: true,
	}
	s.backends[config.ID] = backend

	// Shared ingress may still hold replies after this backend has been removed.
	srv.SetSender(func(pkt model.Packet) {
		if !backend.present {
			return
		}
		originalClient := s.lb.GetClient(pkt.Key)
		transmitPacket(backend.returnLink, pkt, func(pkt model.Packet) {
			if !backend.present {
				return
			}
			transmitPacket(s.backendIngress, pkt, func(pkt model.Packet) {
				if backend.present && s.lb.HasClient(originalClient) {
					s.lb.HandleServerPacket(srv, pkt)
				}
			})
		})
	})

	// Old mappings keep this sender. Removal makes it drop rather than select again.
	err := s.lb.AddServer(srv, func(pkt model.Packet) {
		if !backend.present {
			return
		}
		originalClient := s.lb.GetClient(pkt.Key)
		transmitPacket(backend.output, pkt, func(pkt model.Packet) {
			if backend.present && s.lb.HasClient(originalClient) {
				srv.HandlePacketIf(pkt, func() bool {
					return backend.present && s.lb.HasClient(originalClient)
				})
			}
		})
	})
	if err != nil {
		panic(err) // Timeline identities were checked before any world was created.
	}
	srv.Start()
}

// removeBackend discards local work while independent clients and mappings age normally.
func (s *World) removeBackend(id string) {
	backend := s.backends[id]
	backend.present = false
	backend.output.Retire()
	backend.returnLink.Retire()
	s.lb.RetireServer(backend.server)
}
