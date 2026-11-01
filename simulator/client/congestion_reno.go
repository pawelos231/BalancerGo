package client

import "load-balancer/model"

// renoPolicy retains classic Reno's temporary inflation while a missing segment is repaired.
// Shared TCP detects the third duplicate ACK and owns the retransmission itself.
type renoPolicy struct {
	classicWindow
	activeRecovery      bool   // A fast retry is waiting for its first advancing ACK.
	remainingInflations uint64 // How many further duplicate ACKs may add one MSS to cwnd.
}

// onACK exits recovery on its first advancing ACK, including a partial one.
// With ssthresh=4096, that ACK leaves cwnd=4096 without another growth increment.
func (policy *renoPolicy) onACK(ack congestionACK) {
	if policy.activeRecovery {
		policy.stopRecovery()
		policy.congestionWindowBytes = policy.slowStartThresholdBytes
		policy.avoidanceAcknowledgedBytes = 0
		return
	}
	policy.classicWindow.onACK(ack)
}

// onDuplicateACK permits another MSS after fast retransmit has already retried the head.
// For example, the fourth duplicate can grow cwnd from six to seven MSS.
func (policy *renoPolicy) onDuplicateACK() {
	if policy.activeRecovery && policy.remainingInflations > 0 {
		policy.remainingInflations--
		policy.congestionWindowBytes = addCongestionBytes(policy.congestionWindowBytes, model.SEGMENT_SIZE)
	}
}

// onLoss applies RFC 5681's different window responses to fast loss and timeout.
// https://www.rfc-editor.org/rfc/rfc5681.html#section-3.2
func (policy *renoPolicy) onLoss(loss congestionLoss) {
	switch loss.kind {
	case fastRetransmitLoss:
		policy.slowStartThresholdBytes = max(loss.flightBytes/2, 2*model.SEGMENT_SIZE)
		policy.congestionWindowBytes = addCongestionBytes(policy.slowStartThresholdBytes, 3*model.SEGMENT_SIZE)
		policy.avoidanceAcknowledgedBytes = 0
		policy.activeRecovery = true
		policy.remainingInflations = uint64(max(loss.pendingSegments-3, 0))
	case timeoutLoss:
		policy.stopRecovery()
		policy.restartAfterLoss(loss.flightBytes, loss.alreadyTimedOut)
	case handshakeLoss:
		policy.restartAfterHandshakeLoss()
	}
}

func (policy *renoPolicy) stopRecovery() {
	policy.activeRecovery = false
	policy.remainingInflations = 0
}

func (policy *renoPolicy) state() congestionPolicyState {
	state := policy.classicWindow.state()
	if policy.activeRecovery {
		state.phase = "fastRecovery"
	}
	return state
}
