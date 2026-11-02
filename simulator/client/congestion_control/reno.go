package congestioncontrol

import "load-balancer/model"

// renoPolicy retains classic Reno's temporary inflation while a missing segment is repaired.
// Shared TCP detects the third duplicate ACK and owns the retransmission itself.
type renoPolicy struct {
	classicWindow
	activeRecovery      bool   // A fast retry is waiting for its first advancing ACK.
	remainingInflations uint64 // How many further duplicate ACKs may add one MSS to cwnd.
}

// OnACK exits recovery on its first advancing ACK, including a partial one.
// With ssthresh=4096, that ACK leaves cwnd=4096 without another growth increment.
func (policy *renoPolicy) OnACK(ack ACK) ACKResponse {
	if policy.activeRecovery {
		policy.StopRecovery()
		policy.congestionWindowBytes = policy.slowStartThresholdBytes
		policy.avoidanceAcknowledgedBytes = 0
		return ACKResponse{}
	}
	return policy.classicWindow.OnACK(ack)
}

// OnDuplicateACK permits another MSS after fast retransmit has already retried the head.
// For example, the fourth duplicate can grow cwnd from six to seven MSS.
func (policy *renoPolicy) OnDuplicateACK() {
	if policy.activeRecovery && policy.remainingInflations > 0 {
		policy.remainingInflations--
		policy.congestionWindowBytes = addCongestionBytes(policy.congestionWindowBytes, model.SEGMENT_SIZE)
	}
}

// OnLoss applies RFC 5681's different window responses to fast loss and timeout.
// https://www.rfc-editor.org/rfc/rfc5681.html#section-3.2
func (policy *renoPolicy) OnLoss(loss Loss) LossResponse {
	switch loss.Kind {
	case FastRetransmitLoss:
		policy.slowStartThresholdBytes = max(loss.FlightBytes/2, 2*model.SEGMENT_SIZE)
		policy.congestionWindowBytes = addCongestionBytes(policy.slowStartThresholdBytes, 3*model.SEGMENT_SIZE)
		policy.avoidanceAcknowledgedBytes = 0
		policy.activeRecovery = true
		policy.remainingInflations = uint64(max(loss.PendingSegments-3, 0))
	case TimeoutLoss:
		policy.StopRecovery()
		policy.restartAfterLoss(loss.FlightBytes, loss.AlreadyTimedOut)
	case HandshakeLoss:
		policy.restartAfterHandshakeLoss()
	}
	return LossResponse{}
}

func (policy *renoPolicy) StopRecovery() {
	policy.activeRecovery = false
	policy.remainingInflations = 0
}

func (policy *renoPolicy) State() State {
	state := policy.classicWindow.State()
	if policy.activeRecovery {
		state.Phase = "fastRecovery"
	}
	return state
}
