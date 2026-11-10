package tahoe

import (
	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

// New builds a Tahoe policy from settings already validated by the factory.
func New(config model.CongestionConfig, mssBytes int) congestioncontrol.Policy {
	return &tahoePolicy{ClassicWindow: congestioncontrol.NewClassicWindow(config, mssBytes)}
}

// tahoePolicy uses the same configured window growth as Reno, but restarts slow start
// after fast retransmit instead of inflating the window during fast recovery.
type tahoePolicy struct {
	congestioncontrol.ClassicWindow
}

// OnLoss retries with cwnd=1 MSS after either fast loss or an RTO.
// Later duplicate ACKs do not inflate Tahoe's window; a new ACK grows it normally.
// https://www.rfc-editor.org/rfc/rfc3390.html#section-6
func (policy *tahoePolicy) OnLoss(loss congestioncontrol.Loss) congestioncontrol.LossResponse {
	switch loss.Kind {
	case congestioncontrol.FastRetransmitLoss:
		if !loss.AlreadyReduced {
			policy.RestartAfterLoss(loss.FlightBytes, false)
		}
	case congestioncontrol.TimeoutLoss:
		policy.RestartAfterLoss(loss.FlightBytes, loss.AlreadyTimedOut || loss.AlreadyReduced)
	case congestioncontrol.HandshakeLoss:
		policy.RestartAfterHandshakeLoss()
	}
	return congestioncontrol.LossResponse{}
}
