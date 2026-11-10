package receiver

import (
	"time"

	"load-balancer/model"
)

// SetIdleTimeouts chooses local cleanup limits before traffic; zero disables a limit.
func (r *Receiver) SetIdleTimeouts(halfOpen, established time.Duration) {
	if halfOpen < 0 || established < 0 {
		panic("tcp: idle timeouts must be nonnegative")
	}
	if r.trafficStarted || len(r.connections) != 0 {
		panic("tcp: idle timeouts must be set before traffic")
	}

	r.halfOpenIdleTimeout = halfOpen
	r.establishedIdleTimeout = established
}

// recordConnectionActivity moves the established deadline after valid peer traffic.
// For example, traffic at 8 s with a 10 s limit keeps the connection until 18 s.
func (r *Receiver) recordConnectionActivity(key model.ClientKey, conn *receiveConnection) {
	if r.connections[key] != conn || conn.state != model.TCPEstablished {
		return
	}

	conn.lastActivity = r.engine.Now()
	if conn.idleEvent == 0 {
		r.scheduleIdleTimeout(key, conn, r.establishedIdleTimeout)
	}
}

// scheduleIdleTimeout keeps one pending check; incoming data only updates its timestamp.
func (r *Receiver) scheduleIdleTimeout(key model.ClientKey, conn *receiveConnection, delay time.Duration) {
	if delay == 0 {
		return
	}

	conn.idleEvent = r.engine.ScheduleAfter(delay, func() {
		if r.connections[key] != conn {
			return
		}
		conn.idleEvent = 0

		limit := r.halfOpenIdleTimeout
		if conn.state == model.TCPEstablished {
			limit = r.establishedIdleTimeout
		} else if conn.state != model.TCPSynReceived {
			return
		}

		// Recent traffic postpones this check without a new timer for every packet.
		remaining := limit - (r.engine.Now() - conn.lastActivity)
		if remaining > 0 {
			r.scheduleIdleTimeout(key, conn, remaining)
			return
		}

		// Only this backend's reservation expires; the peer and LB keep their state.
		r.finishConnection(key, conn, model.CloseAborted)
	})
}

func (r *Receiver) cancelIdleTimer(conn *receiveConnection) {
	r.engine.Cancel(conn.idleEvent)
	conn.idleEvent = 0
}
