package congestioncontrol

import "load-balancer/model"

// newRenoPolicy keeps repairing one flight until its final byte is acknowledged.
// It follows RFC 6582 sections 3.2 and 4 without the optional loss heuristics.
type newRenoPolicy struct {
	classicWindow
	activeRecovery      bool
	recoveryEndSeq      uint64 // Exclusive boundary; retained after recovery and RTO.
	remainingInflations uint64
	partialACKSeen      bool
	recoveryWindowBytes uint64
}

func newNewRenoPolicy(config model.CongestionConfig) *newRenoPolicy {
	return &newRenoPolicy{
		classicWindow:  newClassicWindow(config),
		recoveryEndSeq: 1, // The SYN occupies sequence zero.
	}
}

// OnACK retries the next hole on a classic NewReno partial ACK; SACK selects its own retries.
// Full ACKs use RFC 6582's first deflation option to limit the next burst.
func (policy *newRenoPolicy) OnACK(ack ACK) ACKResponse {
	if !policy.activeRecovery {
		return policy.classicWindow.OnACK(ack)
	}
	// SACK chooses its own holes; a partial ACK must not also request NewReno's retry.
	if ack.SACKRecovery {
		if ack.EndSACKRecovery {
			policy.StopRecovery()
			policy.setWindowBytes(policy.recoveryWindowBytes)
			policy.avoidanceAcknowledgedBytes = 0
		}
		return ACKResponse{}
	}
	if ack.CumulativeACK >= policy.recoveryEndSeq {
		policy.StopRecovery()
		remainingFlight := max(ack.FlightBytes, uint64(model.SEGMENT_SIZE))
		flightWindow := addCongestionBytes(remainingFlight, model.SEGMENT_SIZE)
		policy.setWindowBytes(min(policy.recoveryWindowBytes, flightWindow))
		policy.avoidanceAcknowledgedBytes = 0
		return ACKResponse{}
	}

	policy.setWindowBytes(policy.congestionWindowBytes - min(policy.congestionWindowBytes, ack.NewBytes))
	if ack.NewBytes >= model.SEGMENT_SIZE {
		policy.setWindowBytes(addCongestionBytes(
			policy.congestionWindowBytes, model.SEGMENT_SIZE,
		))
	}
	policy.setWindowBytes(max(policy.congestionWindowBytes, uint64(model.SEGMENT_SIZE)))
	response := ACKResponse{
		RetransmitOldest:        true,
		PreserveRetransmitTimer: policy.partialACKSeen,
	}
	policy.partialACKSeen = true
	return response
}

// OnDuplicateACK bounds inflation by the packets outstanding at recovery entry.
func (policy *newRenoPolicy) OnDuplicateACK() {
	if policy.activeRecovery && policy.remainingInflations > 0 {
		policy.remainingInflations--
		policy.setWindowBytes(addCongestionBytes(
			policy.congestionWindowBytes, model.SEGMENT_SIZE,
		))
	}
}

// OnLoss uses NewReno's old-flight guard unless TCP manages SACK recovery.
// Without SACK, loss of the first data byte waits for RTO in this conservative profile.
func (policy *newRenoPolicy) OnLoss(loss Loss) LossResponse {
	switch loss.Kind {
	case FastRetransmitLoss:
		// The SACK sender owns its recovery boundary, including a loss of the first byte.
		oldNewRenoFlight := !loss.SACKRecovery && !loss.AlreadyReduced && loss.CumulativeACK <= policy.recoveryEndSeq
		if policy.activeRecovery || oldNewRenoFlight {
			return LossResponse{SuppressFastRetransmit: true}
		}
		policy.recoveryEndSeq = loss.NextSendSeq
		if !loss.AlreadyReduced {
			policy.slowStartThresholdBytes = max(loss.FlightBytes/2, 2*model.SEGMENT_SIZE)
			policy.recoveryWindowBytes = policy.slowStartThresholdBytes
		} else {
			policy.recoveryWindowBytes = policy.congestionWindowBytes
		}
		policy.setWindowBytes(policy.recoveryWindowBytes)
		policy.avoidanceAcknowledgedBytes = 0
		policy.activeRecovery = true
		policy.remainingInflations = 0
		policy.partialACKSeen = false

		// RFC 6675 accounts for data still in transit with Pipe rather than inflation.
		if !loss.SACKRecovery {
			policy.setWindowBytes(addCongestionBytes(
				policy.recoveryWindowBytes, 3*model.SEGMENT_SIZE,
			))
			policy.remainingInflations = uint64(max(loss.PendingSegments-3, 0))
		}
	case TimeoutLoss:
		policy.StopRecovery()
		policy.recoveryEndSeq = loss.NextSendSeq
		policy.restartAfterLoss(loss.FlightBytes, loss.AlreadyTimedOut || loss.AlreadyReduced)
	case HandshakeLoss:
		policy.StopRecovery()
		policy.restartAfterHandshakeLoss()
	}
	return LossResponse{}
}

func (policy *newRenoPolicy) OnECN(ecn ECN) {
	policy.classicWindow.OnECN(ecn)
	if policy.activeRecovery {
		policy.recoveryWindowBytes = policy.congestionWindowBytes
	}
}

func (policy *newRenoPolicy) StopRecovery() {
	policy.activeRecovery = false
	policy.remainingInflations = 0
	policy.partialACKSeen = false
}

func (policy *newRenoPolicy) State() State {
	state := policy.classicWindow.State()
	if policy.activeRecovery {
		state.Phase = "fastRecovery"
	}
	return state
}
