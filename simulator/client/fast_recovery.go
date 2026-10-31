package client

import "load-balancer/model"

type fastRecovery struct {
	duplicateACKs       uint64
	fastRetransmissions uint64 // Total segments retransmitted because of duplicate ACKs instead of a timeout.
	active              bool
	untilSeq            uint64 // ACK must reach this byte position before duplicate ACKs can start another recovery.
	remainingInflations uint64 // How many more duplicate ACKs may increase cwnd by one segment during recovery.

}

// duplicateACK applies classic Reno's RFC 5681 section 3.2 window rules.
// flight counts sent payload bytes still waiting for ACK: 8 x 1024 B sent, 2 ACKed = 6144 B.
// pendingCount counts segments still waiting for ACK: 8 sent, 2 ACKed = 6 pending.
// It limits how many duplicate ACKs may increase cwnd during recovery.
// Three ACK=1 replies retry [1,1025); later duplicates may permit new data.
// https://www.rfc-editor.org/rfc/rfc5681.html#section-3.2
func (control *congestionControl) duplicateACK(flight uint64, pendingCount int, nextSendSeq uint64) bool {
	if !control.config.FastRetransmit || flight == 0 {
		return false
	}

	recovery := &control.recovery
	recovery.duplicateACKs++

	// Fast retransmit already retried the missing segment; another duplicate ACK arrived.
	// Example: the fourth duplicate ACK can raise cwnd from 6 to 7 MSS,
	// allowing new data to be sent while we wait for the missing segment's ACK.
	if recovery.active {
		if recovery.remainingInflations > 0 {
			recovery.remainingInflations--
			control.congestionWindowBytes = addCongestionBytes(
				control.congestionWindowBytes,
				model.SEGMENT_SIZE,
			)
		}
		return false
	}

	// A partial ACK or RTO cannot make delayed ACKs from this same flight a new loss.
	if recovery.untilSeq != 0 || recovery.duplicateACKs != 3 {
		return false
	}

	control.slowStartThresholdBytes = max(flight/2, 2*model.SEGMENT_SIZE)
	control.congestionWindowBytes = addCongestionBytes(
		control.slowStartThresholdBytes,
		3*model.SEGMENT_SIZE,
	)
	control.avoidanceAcknowledgedBytes = 0

	recovery.active = true
	recovery.untilSeq = nextSendSeq
	recovery.remainingInflations = uint64(max(pendingCount-3, 0))

	return true
}

// acknowledgeRecovery exits classic Reno on the first ACK that advances, even partially.
// With ssthresh=4096, ACK=1025 leaves cwnd=4096 without another slow-start increment.
func (control *congestionControl) acknowledgeRecovery(ack uint64) bool {
	recovery := &control.recovery
	recovery.duplicateACKs = 0
	if recovery.untilSeq != 0 && ack >= recovery.untilSeq {
		recovery.untilSeq = 0
	}
	if !recovery.active {
		return false
	}

	recovery.active = false
	recovery.remainingInflations = 0
	control.congestionWindowBytes = control.slowStartThresholdBytes
	control.avoidanceAcknowledgedBytes = 0
	return true
}

// stopFastRecovery abandons the active round while retaining its total retry count.
// The caller can install a new flight boundary after an RTO, or leave none after close.
func (control *congestionControl) stopFastRecovery() {
	control.recovery.active = false
	control.recovery.duplicateACKs = 0
	control.recovery.untilSeq = 0
	control.recovery.remainingInflations = 0
}
