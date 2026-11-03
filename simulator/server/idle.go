package server

import (
	"time"

	"load-balancer/model"
)

// SetIdleTimeouts chooses local cleanup limits before traffic; zero disables a limit.
func (s *Server) SetIdleTimeouts(halfOpen, established time.Duration) {
	if halfOpen < 0 || established < 0 {
		panic("server: idle timeouts must be nonnegative")
	}
	if s.NumberOfHandledPackets != 0 || len(s.connections) != 0 {
		panic("server: idle timeouts must be set before traffic")
	}

	s.halfOpenIdleTimeout = halfOpen
	s.establishedIdleTimeout = established
}

// recordConnectionActivity moves the established deadline after valid peer traffic.
// For example, traffic at 8 s with a 10 s limit keeps the connection until 18 s.
func (s *Server) recordConnectionActivity(key model.ClientKey, conn *connection) {
	if s.connections[key] != conn || conn.state != model.TCPEstablished {
		return
	}

	conn.lastActivity = s.engine.Now()
	if conn.idleEvent == 0 {
		s.scheduleIdleTimeout(key, conn, s.establishedIdleTimeout)
	}
}

// scheduleIdleTimeout keeps one pending check; incoming data only updates its timestamp.
func (s *Server) scheduleIdleTimeout(key model.ClientKey, conn *connection, delay time.Duration) {
	if delay == 0 {
		return
	}

	conn.idleEvent = s.engine.ScheduleAfter(delay, func() {
		if s.connections[key] != conn {
			return
		}
		conn.idleEvent = 0

		limit := s.halfOpenIdleTimeout
		if conn.state == model.TCPEstablished {
			limit = s.establishedIdleTimeout
		} else if conn.state != model.TCPSynReceived {
			return
		}

		// Recent traffic postpones this check without a new timer for every packet.
		remaining := limit - (s.engine.Now() - conn.lastActivity)
		if remaining > 0 {
			s.scheduleIdleTimeout(key, conn, remaining)
			return
		}

		// Only this backend's reservation expires; the peer and LB keep their state.
		s.finishConnection(key, conn, model.CloseAborted)
	})
}

func (s *Server) cancelIdleTimer(conn *connection) {
	s.engine.Cancel(conn.idleEvent)
	conn.idleEvent = 0
}
