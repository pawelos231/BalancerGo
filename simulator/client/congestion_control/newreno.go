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
}

func newNewRenoPolicy(config model.CongestionConfig) *newRenoPolicy {
	return &newRenoPolicy{
		classicWindow:  newClassicWindow(config),
		recoveryEndSeq: 1, // The SYN occupies sequence zero.
	}
}

// OnACK retries the next hole on a partial ACK, consuming that ACK's window update.
// Full ACKs use RFC 6582's first deflation option to limit the next burst.
func (policy *newRenoPolicy) OnACK(ack ACK) ACKResponse {
	if !policy.activeRecovery {
		return policy.classicWindow.OnACK(ack)
	}
	if ack.CumulativeACK >= policy.recoveryEndSeq {
		policy.StopRecovery()
		remainingFlight := max(ack.FlightBytes, uint64(model.SEGMENT_SIZE))
		flightWindow := addCongestionBytes(remainingFlight, model.SEGMENT_SIZE)
		policy.congestionWindowBytes = min(policy.slowStartThresholdBytes, flightWindow)
		policy.avoidanceAcknowledgedBytes = 0
		return ACKResponse{}
	}

	policy.congestionWindowBytes -= min(policy.congestionWindowBytes, ack.NewBytes)
	if ack.NewBytes >= model.SEGMENT_SIZE {
		policy.congestionWindowBytes = addCongestionBytes(
			policy.congestionWindowBytes, model.SEGMENT_SIZE,
		)
	}
	policy.congestionWindowBytes = max(policy.congestionWindowBytes, uint64(model.SEGMENT_SIZE))
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
		policy.congestionWindowBytes = addCongestionBytes(
			policy.congestionWindowBytes, model.SEGMENT_SIZE,
		)
	}
}

// OnLoss only accepts a new fast loss after ACKs advance beyond the old flight.
// Loss of the first data byte therefore waits for RTO with this conservative profile.
func (policy *newRenoPolicy) OnLoss(loss Loss) LossResponse {
	switch loss.Kind {
	case FastRetransmitLoss:
		if policy.activeRecovery || loss.CumulativeACK <= policy.recoveryEndSeq {
			return LossResponse{SuppressFastRetransmit: true}
		}
		policy.recoveryEndSeq = loss.NextSendSeq
		policy.slowStartThresholdBytes = max(loss.FlightBytes/2, 2*model.SEGMENT_SIZE)
		policy.congestionWindowBytes = addCongestionBytes(
			policy.slowStartThresholdBytes, 3*model.SEGMENT_SIZE,
		)
		policy.avoidanceAcknowledgedBytes = 0
		policy.activeRecovery = true
		policy.remainingInflations = uint64(max(loss.PendingSegments-3, 0))
		policy.partialACKSeen = false
	case TimeoutLoss:
		policy.StopRecovery()
		policy.recoveryEndSeq = loss.NextSendSeq
		policy.restartAfterLoss(loss.FlightBytes, loss.AlreadyTimedOut)
	case HandshakeLoss:
		policy.StopRecovery()
		policy.restartAfterHandshakeLoss()
	}
	return LossResponse{}
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
