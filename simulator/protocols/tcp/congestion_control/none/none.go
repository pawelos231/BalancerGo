package none

import (
	"time"

	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

// New builds the rwnd-only policy without imposing a congestion window.
func New() congestioncontrol.Policy {
	return unlimitedPolicy{}
}

// unlimitedPolicy preserves the rwnd-only baseline without imposing a congestion window.
// The frontend exposes this policy as "none" in the congestion-control selector.
type unlimitedPolicy struct{}

func (unlimitedPolicy) OnACK(congestioncontrol.ACK) congestioncontrol.ACKResponse {
	return congestioncontrol.ACKResponse{}
}
func (unlimitedPolicy) OnSenderState(congestioncontrol.SenderState) {}
func (unlimitedPolicy) OnDuplicateACK()                             {}
func (unlimitedPolicy) OnLoss(congestioncontrol.Loss) congestioncontrol.LossResponse {
	return congestioncontrol.LossResponse{}
}
func (unlimitedPolicy) OnECN(congestioncontrol.ECN) {}
func (unlimitedPolicy) OnSent(time.Duration, int)   {}
func (unlimitedPolicy) OnIdle()                     {}
func (unlimitedPolicy) StopRecovery()               {}
func (unlimitedPolicy) State() congestioncontrol.State {
	return congestioncontrol.State{Phase: "disabled"}
}
