package congestioncontrol

import "load-balancer/model"

// renoPolicy retains classic Reno's temporary inflation while a missing segment is repaired.
// Shared TCP detects the third duplicate ACK and owns the retransmission itself.
type renoPolicy struct {
	classicWindow
	activeRecovery      bool   // Classic Reno waits for ACK progress; SACK waits for the old flight.
	remainingInflations uint64 // How many further duplicate ACKs may add one MSS to cwnd.
}

// OnACK exits classic recovery on its first advancing ACK; SACK waits for the full flight.
// With ssthresh=4096, that ACK leaves cwnd=4096 without another growth increment.
func (policy *renoPolicy) OnACK(ack ACK) ACKResponse {
	if policy.activeRecovery {
		// A partial ACK can leave another hole; SACK recovery waits for the whole flight.
		if ack.SACKRecovery && !ack.EndSACKRecovery {
			return ACKResponse{}
		}
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

// OnLoss applies the loss reduction; SACK uses RFC 6675's uninflated recovery window.
// https://www.rfc-editor.org/rfc/rfc5681.html#section-3.2
func (policy *renoPolicy) OnLoss(loss Loss) LossResponse {
	switch loss.Kind {
	case FastRetransmitLoss:
		policy.slowStartThresholdBytes = max(loss.FlightBytes/2, 2*model.SEGMENT_SIZE)
		policy.congestionWindowBytes = policy.slowStartThresholdBytes
		policy.avoidanceAcknowledgedBytes = 0
		policy.activeRecovery = true
		policy.remainingInflations = 0

		// Classic Reno uses inflated cwnd; RFC 6675 uses the reduced window and Pipe.
		if !loss.SACKRecovery {
			policy.congestionWindowBytes = addCongestionBytes(policy.slowStartThresholdBytes, 3*model.SEGMENT_SIZE)
			policy.remainingInflations = uint64(max(loss.PendingSegments-3, 0))
		}
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
