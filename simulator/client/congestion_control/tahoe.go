package congestioncontrol

// tahoePolicy uses the same configured window growth as Reno, but restarts slow start
// after fast retransmit instead of inflating the window during fast recovery.
type tahoePolicy struct {
	classicWindow
}

// OnLoss retries with cwnd=1 MSS after either fast loss or an RTO.
// Later duplicate ACKs do not inflate Tahoe's window; a new ACK grows it normally.
// https://www.rfc-editor.org/rfc/rfc3390.html#section-6
func (policy *tahoePolicy) OnLoss(loss Loss) {
	switch loss.Kind {
	case FastRetransmitLoss:
		policy.restartAfterLoss(loss.FlightBytes, false)
	case TimeoutLoss:
		policy.restartAfterLoss(loss.FlightBytes, loss.AlreadyTimedOut)
	case HandshakeLoss:
		policy.restartAfterHandshakeLoss()
	}
}
