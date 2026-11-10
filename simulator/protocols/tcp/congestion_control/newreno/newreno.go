package newreno

import (
	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

// newRenoPolicy keeps repairing one flight until its final byte is acknowledged.
// It follows RFC 6582 sections 3.2 and 4 without the optional loss heuristics.
type newRenoPolicy struct {
	congestioncontrol.ClassicWindow
	activeRecovery      bool
	recoveryEndSeq      uint64 // Exclusive boundary; retained after recovery and RTO.
	remainingInflations uint64
	partialACKSeen      bool
	recoveryWindowBytes uint64
}

// New builds a NewReno policy from settings already validated by the factory.
func New(config model.CongestionConfig, mssBytes int) congestioncontrol.Policy {
	return newNewRenoPolicy(config, mssBytes)
}

func newNewRenoPolicy(config model.CongestionConfig, mssBytes int) *newRenoPolicy {
	return &newRenoPolicy{
		ClassicWindow:  congestioncontrol.NewClassicWindow(config, mssBytes),
		recoveryEndSeq: 1, // The SYN occupies sequence zero.
	}
}

// OnACK retries the next hole on a classic NewReno partial ACK; SACK selects its own retries.
// Full ACKs use RFC 6582's first deflation option to limit the next burst.
func (policy *newRenoPolicy) OnACK(ack congestioncontrol.ACK) congestioncontrol.ACKResponse {
	if !policy.activeRecovery {
		return policy.ClassicWindow.OnACK(ack)
	}
	// SACK chooses its own holes; a partial ACK must not also request NewReno's retry.
	if ack.SACKRecovery {
		if ack.EndSACKRecovery {
			policy.StopRecovery()
			policy.SetWindowBytes(policy.recoveryWindowBytes)
			policy.AvoidanceAcknowledgedBytes = 0
		}
		return congestioncontrol.ACKResponse{}
	}
	if ack.CumulativeACK >= policy.recoveryEndSeq {
		policy.StopRecovery()
		remainingFlight := max(ack.FlightBytes, policy.MSSBytes)
		flightWindow := congestioncontrol.AddCongestionBytes(remainingFlight, policy.MSSBytes)
		policy.SetWindowBytes(min(policy.recoveryWindowBytes, flightWindow))
		policy.AvoidanceAcknowledgedBytes = 0
		return congestioncontrol.ACKResponse{}
	}

	policy.SetWindowBytes(policy.CongestionWindowBytes - min(policy.CongestionWindowBytes, ack.NewBytes))
	if ack.NewBytes >= policy.MSSBytes {
		policy.SetWindowBytes(congestioncontrol.AddCongestionBytes(
			policy.CongestionWindowBytes, policy.MSSBytes,
		))
	}
	policy.SetWindowBytes(max(policy.CongestionWindowBytes, policy.MSSBytes))
	response := congestioncontrol.ACKResponse{
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
		policy.SetWindowBytes(congestioncontrol.AddCongestionBytes(
			policy.CongestionWindowBytes, policy.MSSBytes,
		))
	}
}

// OnLoss uses NewReno's old-flight guard unless TCP manages SACK recovery.
// Without SACK, loss of the first data byte waits for RTO in this conservative profile.
func (policy *newRenoPolicy) OnLoss(loss congestioncontrol.Loss) congestioncontrol.LossResponse {
	switch loss.Kind {
	case congestioncontrol.FastRetransmitLoss:
		// The SACK sender owns its recovery boundary, including a loss of the first byte.
		oldNewRenoFlight := !loss.SACKRecovery && !loss.AlreadyReduced && loss.CumulativeACK <= policy.recoveryEndSeq
		if policy.activeRecovery || oldNewRenoFlight {
			return congestioncontrol.LossResponse{SuppressFastRetransmit: true}
		}
		policy.recoveryEndSeq = loss.NextSendSeq
		if !loss.AlreadyReduced {
			policy.SlowStartThresholdBytes = max(loss.FlightBytes/2, 2*policy.MSSBytes)
			policy.recoveryWindowBytes = policy.SlowStartThresholdBytes
		} else {
			policy.recoveryWindowBytes = policy.CongestionWindowBytes
		}
		policy.SetWindowBytes(policy.recoveryWindowBytes)
		policy.AvoidanceAcknowledgedBytes = 0
		policy.activeRecovery = true
		policy.remainingInflations = 0
		policy.partialACKSeen = false

		// RFC 6675 accounts for data still in transit with Pipe rather than inflation.
		if !loss.SACKRecovery {
			policy.SetWindowBytes(congestioncontrol.AddCongestionBytes(
				policy.recoveryWindowBytes, 3*policy.MSSBytes,
			))
			policy.remainingInflations = uint64(max(loss.PendingSegments-3, 0))
		}
	case congestioncontrol.TimeoutLoss:
		policy.StopRecovery()
		policy.recoveryEndSeq = loss.NextSendSeq
		policy.RestartAfterLoss(loss.FlightBytes, loss.AlreadyTimedOut || loss.AlreadyReduced)
	case congestioncontrol.HandshakeLoss:
		policy.StopRecovery()
		policy.RestartAfterHandshakeLoss()
	}
	return congestioncontrol.LossResponse{}
}

func (policy *newRenoPolicy) OnECN(ecn congestioncontrol.ECN) {
	policy.ClassicWindow.OnECN(ecn)
	if policy.activeRecovery {
		policy.recoveryWindowBytes = policy.CongestionWindowBytes
	}
}

func (policy *newRenoPolicy) StopRecovery() {
	policy.activeRecovery = false
	policy.remainingInflations = 0
	policy.partialACKSeen = false
}

func (policy *newRenoPolicy) State() congestioncontrol.State {
	state := policy.ClassicWindow.State()
	if policy.activeRecovery {
		state.Phase = "fastRecovery"
	}
	return state
}
