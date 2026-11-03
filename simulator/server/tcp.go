package server

import (
	"time"

	"load-balancer/model"
)

// HandlePacket applies host availability before handing network data to TCP.
func (s *Server) HandlePacket(packet model.Packet) {
	if s.Killed || !s.Active || packet.Key.Protocol != model.TCP {
		return
	}
	s.NumberOfHandledPackets++
	s.transport.HandlePacket(packet)
}

func (s *Server) SetReceiveBufferBytes(capacity int) {
	s.transport.SetReceiveBufferBytes(capacity)
}

func (s *Server) SetSACKEnabled(enabled bool) {
	s.transport.SetSACKEnabled(enabled)
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
