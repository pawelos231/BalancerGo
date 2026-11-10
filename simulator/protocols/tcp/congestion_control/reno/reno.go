package reno

import (
	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

// New builds a Reno policy from settings already validated by the factory.
func New(config model.CongestionConfig, mssBytes int) congestioncontrol.Policy {
	return &renoPolicy{ClassicWindow: congestioncontrol.NewClassicWindow(config, mssBytes)}
}

// renoPolicy retains classic Reno's temporary inflation while a missing segment is repaired.
// Shared TCP detects the third duplicate ACK and owns the retransmission itself.
type renoPolicy struct {
	congestioncontrol.ClassicWindow
	activeRecovery      bool   // Classic Reno waits for ACK progress; SACK waits for the old flight.
	remainingInflations uint64 // How many further duplicate ACKs may add one MSS to cwnd.
	recoveryWindowBytes uint64 // Uninflated window retained through a loss after ECN.
}

// OnACK exits classic recovery on its first advancing ACK; SACK waits for the full flight.
// With ssthresh=4096, that ACK leaves cwnd=4096 without another growth increment.
func (policy *renoPolicy) OnACK(ack congestioncontrol.ACK) congestioncontrol.ACKResponse {
	if policy.activeRecovery {
		// A partial ACK can leave another hole; SACK recovery waits for the whole flight.
		if ack.SACKRecovery && !ack.EndSACKRecovery {
			return congestioncontrol.ACKResponse{}
		}
		policy.StopRecovery()
		policy.SetWindowBytes(policy.recoveryWindowBytes)
		policy.AvoidanceAcknowledgedBytes = 0
		return congestioncontrol.ACKResponse{}
	}
	return policy.ClassicWindow.OnACK(ack)
}

// OnDuplicateACK permits another MSS after fast retransmit has already retried the head.
// For example, the fourth duplicate can grow cwnd from six to seven MSS.
func (policy *renoPolicy) OnDuplicateACK() {
	if policy.activeRecovery && policy.remainingInflations > 0 {
		policy.remainingInflations--
		policy.SetWindowBytes(congestioncontrol.AddCongestionBytes(
			policy.CongestionWindowBytes, policy.MSSBytes,
		))
	}
}

// OnLoss applies the loss reduction; SACK uses RFC 6675's uninflated recovery window.
// https://www.rfc-editor.org/rfc/rfc5681.html#section-3.2
func (policy *renoPolicy) OnLoss(loss congestioncontrol.Loss) congestioncontrol.LossResponse {
	switch loss.Kind {
	case congestioncontrol.FastRetransmitLoss:
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

		// Classic Reno uses inflated cwnd; RFC 6675 uses the reduced window and Pipe.
		if !loss.SACKRecovery {
			policy.SetWindowBytes(congestioncontrol.AddCongestionBytes(
				policy.recoveryWindowBytes, 3*policy.MSSBytes,
			))
			policy.remainingInflations = uint64(max(loss.PendingSegments-3, 0))
		}
	case congestioncontrol.TimeoutLoss:
		policy.StopRecovery()
		policy.RestartAfterLoss(loss.FlightBytes, loss.AlreadyTimedOut || loss.AlreadyReduced)
	case congestioncontrol.HandshakeLoss:
		policy.RestartAfterHandshakeLoss()
	}
	return congestioncontrol.LossResponse{}
}

func (policy *renoPolicy) OnECN(ecn congestioncontrol.ECN) {
	policy.ClassicWindow.OnECN(ecn)
	if policy.activeRecovery {
		policy.recoveryWindowBytes = policy.CongestionWindowBytes
	}
}

func (policy *renoPolicy) StopRecovery() {
	policy.activeRecovery = false
	policy.remainingInflations = 0
}

func (policy *renoPolicy) State() congestioncontrol.State {
	state := policy.ClassicWindow.State()
	if policy.activeRecovery {
		state.Phase = "fastRecovery"
	}
	return state
}
