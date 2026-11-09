package server

import (
	"time"

	"load-balancer/host"
	"load-balancer/model"
)

// HandlePacket applies host availability before handing network data to TCP.
func (s *Server) HandlePacket(packet model.Packet) {
	s.HandlePacketIf(packet, nil)
}

// HandlePacketIf rechecks delivery ownership after the packet waits for CPU.
// A delayed packet must not reach a replacement client using the same tuple.
func (s *Server) HandlePacketIf(packet model.Packet, valid func() bool) {
	if s.Killed || !s.Active || packet.Key.Protocol != model.TCP {
		return
	}
	s.processor.Submit(func() {
		if s.Killed || !s.Active || (valid != nil && !valid()) {
			return
		}
		s.NumberOfHandledPackets++
		s.transport.HandlePacket(packet)
	})
}

// SetHostConfig chooses this backend's CPU capacity before it starts receiving packets.
func (s *Server) SetHostConfig(config host.Config) {
	if s.started || s.Killed || s.processor.Stats().OfferedPackets != 0 {
		panic("server: host configuration must be set before traffic starts")
	}
	processor := host.New(s.engine, config)
	s.processor.Stop()
	s.processor = processor
}

func (s *Server) HostStats() host.Stats { return s.processor.Stats() }

// PrepareHostConfig edits CPU limits while preserving packet work and TCP state.
func (s *Server) PrepareHostConfig(config host.Config) host.PreparedConfig {
	return s.processor.PrepareConfig(config)
}

func (s *Server) SetReceiveBufferBytes(capacity int) {
	s.transport.SetReceiveBufferBytes(capacity)
}

func (s *Server) SetSACKEnabled(enabled bool) {
	s.transport.SetSACKEnabled(enabled)
}

func (s *Server) SetECNEnabled(enabled bool) {
	s.transport.SetECNEnabled(enabled)
}

func (s *Server) GetECNState() model.ECNState {
	return s.transport.GetECNState()
}

func (s *Server) SetDelayedACKConfig(config model.DelayedACKConfig) {
	s.transport.SetDelayedACKConfig(config)
}

func (s *Server) GetACKState() model.ACKState {
	return s.transport.GetACKState()
}

func (s *Server) SetCloseConfig(config model.CloseConfig) {
	s.transport.SetCloseConfig(config)
}

func (s *Server) SetRTOConfig(config model.RTOConfig) {
	s.transport.SetRTOConfig(config)
}

func (s *Server) SetIdleTimeouts(halfOpen, established time.Duration) {
	s.transport.SetIdleTimeouts(halfOpen, established)
}

func (s *Server) GetConnectionState(key model.ClientKey) model.CloseState {
	return s.transport.GetConnectionState(key)
}

func (s *Server) CloseConnection(key model.ClientKey) {
	s.transport.CloseConnection(key)
}

func (s *Server) AbortConnection(key model.ClientKey) {
	s.transport.AbortConnection(key)
}

func (s *Server) ForgetConnection(key model.ClientKey) {
	s.transport.ForgetConnection(key)
}
