// Package congestioncontrol updates sender windows from TCP events and local measurements.
// TCP keeps ownership of packets, buffers, timers and actual retransmissions.
package congestioncontrol

import "time"

// Policy receives transport facts, never a Client or an engine.
// It controls the send window and an optional virtual deadline for new data.
type Policy interface {
	// New payload bytes were ACKed; update the window and phase.
	// Example: Reno in slow start grows cwnd from 4 to 5 MSS after a 1-MSS ACK.
	OnACK(ACK)

	// Observe whether workload gaps or rwnd prevent using the congestion window.
	// Example: CUBIC pauses its growth clock during a known 10 ms workload gap.
	OnSenderState(SenderState)

	// A valid duplicate ACK arrived without confirming new bytes.
	// Example: Reno may add one MSS to cwnd while already in fast recovery.
	OnDuplicateACK()

	// React to loss detected by three duplicate ACKs, an RTO, or a SYN retry.
	// Example: an RTO makes Reno set cwnd to one MSS; TCP retransmits separately.
	OnLoss(Loss)

	// Data is about to be sent or retried; use its virtual send time and size if needed.
	// Example: Reno does nothing; a future BBR policy could plan its next send time.
	OnSent(now time.Duration, payloadBytes int)

	// Sending resumes after more than one RTO without any data transmissions.
	// Example: Reno reduces a 16-MSS window to its configured initial 4 MSS.
	OnIdle()

	// Clear the algorithm's recovery state; shared TCP counters are handled separately.
	// Example: Reno leaves fast recovery and clears its remaining cwnd increases.
	StopRecovery()

	// Read the current window, phase and optional virtual send deadline without changing them.
	// Example: Reno can report cwnd=4096 bytes and Phase="slowStart".
	State() State
}

// State is read by the sender and the public state projector.
// NextSendAt=0 adds no pacing; retries retain their immediate transport path.
type State struct {
	WindowBytes    uint64
	ThresholdBytes uint64
	Phase          string
	LimitsNewData  bool
	NextSendAt     time.Duration
}

// ACK contains sender measurements, not information about remote links.
type ACK struct {
	At          time.Duration
	NewBytes    uint64
	SmoothedRTT time.Duration
}

// SenderState describes why new data might have to wait.
// ReadyAt ends a known workload gap; no backlog or a small rwnd can last longer.
type SenderState struct {
	At                 time.Duration
	ReadyAt            time.Duration
	HasData            bool
	FlightBytes        uint64
	ReceiveWindowBytes uint64
}

// LossKind tells the policy whether loss was detected by ACKs, RTO or handshake retry.
type LossKind uint8

const (
	FastRetransmitLoss LossKind = iota
	TimeoutLoss
	HandshakeLoss
)

// Loss tells the algorithm how loss was detected and how much data was in flight.
type Loss struct {
	At              time.Duration
	Kind            LossKind
	FlightBytes     uint64
	PendingSegments int
	AlreadyTimedOut bool
}

// unlimitedPolicy preserves the rwnd-only baseline without imposing a congestion window.
// The frontend exposes this policy as "none" in the congestion-control selector.
type unlimitedPolicy struct{}

func (unlimitedPolicy) OnACK(ACK)                 {}
func (unlimitedPolicy) OnSenderState(SenderState) {}
func (unlimitedPolicy) OnDuplicateACK()           {}
func (unlimitedPolicy) OnLoss(Loss)               {}
func (unlimitedPolicy) OnSent(time.Duration, int) {}
func (unlimitedPolicy) OnIdle()                   {}
func (unlimitedPolicy) StopRecovery()             {}
func (unlimitedPolicy) State() State {
	return State{Phase: "disabled"}
}
