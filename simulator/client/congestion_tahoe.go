package client

// tahoePolicy uses the same configured window growth as Reno, but restarts slow start
// after fast retransmit instead of inflating the window during fast recovery.
type tahoePolicy struct {
	classicWindow
}

// onLoss retries with cwnd=1 MSS after either fast loss or an RTO.
// Later duplicate ACKs do not inflate Tahoe's window; a new ACK grows it normally.
// https://www.rfc-editor.org/rfc/rfc3390.html#section-6
func (policy *tahoePolicy) onLoss(loss congestionLoss) {
	switch loss.kind {
	case fastRetransmitLoss:
		policy.restartAfterLoss(loss.flightBytes, false)
	case timeoutLoss:
		policy.restartAfterLoss(loss.flightBytes, loss.alreadyTimedOut)
	case handshakeLoss:
		policy.restartAfterHandshakeLoss()
	}
}
