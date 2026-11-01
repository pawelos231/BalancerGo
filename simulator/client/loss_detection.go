package client

import (
	"time"

	congestioncontrol "load-balancer/client/congestion_control"
)

// lossDetection tracks duplicate ACKs independently of Reno's recovery window.
// Tahoe needs this same guard and retry count without entering Reno fast recovery.
type lossDetection struct {
	duplicateACKs       uint64
	fastRetransmissions uint64 // Total segments retried by duplicate ACKs rather than a timeout.
	untilSeq            uint64 // ACK must reach this byte position before another fast loss is allowed.
}

// duplicateACK detects an early loss, then asks the selected policy to change its window.
// flight is unconfirmed payload bytes: eight 1024-byte segments minus two ACKed = 6144.
// pendingCount counts segments instead: eight sent minus two ACKed = six pending.
func (control *congestionControl) duplicateACK(
	flight uint64,
	pendingCount int,
	nextSendSeq uint64,
	now time.Duration,
) bool {
	if !control.config.FastRetransmit || flight == 0 {
		return false
	}

	control.loss.duplicateACKs++
	// Reno can inflate its active recovery window; Tahoe ignores this notification.
	control.policy.OnDuplicateACK()
	if control.loss.untilSeq != 0 || control.loss.duplicateACKs != 3 {
		return false
	}

	// Protect this flight even after a partial ACK, so late duplicates cannot cut it again.
	control.loss.untilSeq = nextSendSeq
	control.policy.OnLoss(congestioncontrol.Loss{
		At:              now,
		Kind:            congestioncontrol.FastRetransmitLoss,
		FlightBytes:     flight,
		PendingSegments: pendingCount,
	})
	return true
}
