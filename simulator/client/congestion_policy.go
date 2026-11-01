package client

import "time"

// congestionPolicy receives transport facts, never a Client or an engine.
// It controls the send window and an optional virtual deadline for new data.
type congestionPolicy interface {
	// New payload bytes were ACKed; update the window and phase.
	// Example: Reno in slow start grows cwnd from 4 to 5 MSS after a 1-MSS ACK.
	onACK(congestionACK)

	// Observe whether workload gaps or rwnd prevent using the congestion window.
	// Example: CUBIC pauses its growth clock during a known 10 ms workload gap.
	onSenderState(congestionSenderState)

	// A valid duplicate ACK arrived without confirming new bytes.
	// Example: Reno may add one MSS to cwnd while already in fast recovery.
	onDuplicateACK()

	// React to loss detected by three duplicate ACKs, an RTO, or a SYN retry.
	// Example: an RTO makes Reno set cwnd to one MSS; TCP retransmits separately.
	onLoss(congestionLoss)

	// Data is about to be sent or retried; use its virtual send time and size if needed.
	// Example: Reno does nothing; a future BBR policy could plan its next send time.
	onSent(now time.Duration, payloadBytes int)

	// Sending resumes after more than one RTO without any data transmissions.
	// Example: Reno reduces a 16-MSS window to its configured initial 4 MSS.
	onIdle()

	// Clear the algorithm's recovery state; shared TCP counters are handled separately.
	// Example: Reno leaves fast recovery and clears its remaining cwnd increases.
	stopRecovery()

	// Read the current window, phase and optional virtual send deadline without changing them.
	// Example: Reno can report cwnd=4096 bytes and phase="slowStart".
	state() congestionPolicyState
}

// congestionPolicyState is read by the sender and the public state projector.
// nextSendAt=0 adds no pacing; retries retain their immediate transport path.
type congestionPolicyState struct {
	windowBytes    uint64
	thresholdBytes uint64
	phase          string
	limitsNewData  bool
	nextSendAt     time.Duration
}

// congestionACK contains sender measurements, not information about remote links.
type congestionACK struct {
	at          time.Duration
	newBytes    uint64
	smoothedRTT time.Duration
}

// congestionSenderState describes why new data might have to wait.
// readyAt ends a known workload gap; no backlog or a small rwnd can last longer.
type congestionSenderState struct {
	at                 time.Duration
	readyAt            time.Duration
	hasData            bool
	flightBytes        uint64
	receiveWindowBytes uint64
}

// congestionLossKind tells the policy whether loss was detected by ACKs, RTO or handshake retry.
type congestionLossKind uint8

const (
	fastRetransmitLoss congestionLossKind = iota
	timeoutLoss
	handshakeLoss
)

// congestionLoss describes one observed loss and tells the onLoss function what kind of lose it was (with additional metadata)
type congestionLoss struct {
	at              time.Duration
	kind            congestionLossKind
	flightBytes     uint64
	pendingSegments int
	alreadyTimedOut bool
}

// unlimitedPolicy preserves the rwnd-only baseline without imposing a congestion window.
// The frontend exposes this policy as "none" in the congestion-control selector.
type unlimitedPolicy struct{}

func (unlimitedPolicy) onACK(congestionACK)                 {}
func (unlimitedPolicy) onSenderState(congestionSenderState) {}
func (unlimitedPolicy) onDuplicateACK()                     {}
func (unlimitedPolicy) onLoss(congestionLoss)               {}
func (unlimitedPolicy) onSent(time.Duration, int)           {}
func (unlimitedPolicy) onIdle()                             {}
func (unlimitedPolicy) stopRecovery()                       {}
func (unlimitedPolicy) state() congestionPolicyState {
	return congestionPolicyState{phase: "disabled"}
}
