package client

import (
	"time"

	"load-balancer/model"
)

// Reno's window rules below follow RFC 5681 sections 3.1 and 4.1:
// https://www.rfc-editor.org/rfc/rfc5681.html

// congestionControl limits new data independently of the receiver's free space.
// Reno starts with a small window, grows on new ACKs and reduces it after loss.
type congestionControl struct {
	config                     model.CongestionConfig
	startupWindowBytes         uint64
	congestionWindowBytes      uint64
	slowStartThresholdBytes    uint64
	avoidanceAcknowledgedBytes uint64

	lastDataSentAt time.Duration
	hasSentData    bool

	// Partial ACKs can trim a retry, but its original end still identifies it.
	// Zero means no pending segment has reduced the threshold through an RTO.
	timedOutSegmentEnd uint64
}

// newCongestionControl starts at the configured window, before any data is sent.
func newCongestionControl(config model.CongestionConfig) congestionControl {
	validMode := config.Mode == model.CongestionNone || config.Mode == model.CongestionReno
	validInitialWindow := config.InitialWindowSegments >= 1 &&
		uint64(config.InitialWindowSegments) <= model.MaxCongestionWindowBytes/model.SEGMENT_SIZE
	validThreshold := config.InitialSlowStartThresholdBytes >= 2*model.SEGMENT_SIZE &&
		config.InitialSlowStartThresholdBytes <= model.MaxCongestionWindowBytes
	if !validMode || !validInitialWindow || !validThreshold {
		panic("client: invalid congestion configuration")
	}

	initialWindow := uint64(config.InitialWindowSegments) * model.SEGMENT_SIZE
	return congestionControl{
		config:                  config,
		startupWindowBytes:      initialWindow,
		congestionWindowBytes:   initialWindow,
		slowStartThresholdBytes: config.InitialSlowStartThresholdBytes,
	}
}

// SetCongestionConfig selects sender behavior before the connection starts.
func (c *Client) SetCongestionConfig(config model.CongestionConfig) {
	if c.handshakeStarted || c.terminated {
		panic("client: congestion configuration must be set before the connection starts")
	}

	c.congestion = newCongestionControl(config)
}

// GetCongestionState reports the sender window and payload still awaiting ACKs.
// Disabled control has no congestion window, but its bytes in flight remain visible.
func (c *Client) GetCongestionState() model.CongestionState {
	control := &c.congestion
	state := model.CongestionState{
		Mode:          control.config.Mode,
		BytesInFlight: c.bytesInFlight(),
		Phase:         "disabled",
	}
	if control.config.Mode == model.CongestionNone {
		return state
	}

	state.CongestionWindowBytes = control.congestionWindowBytes
	state.SlowStartThresholdBytes = control.slowStartThresholdBytes
	state.Phase = "congestionAvoidance"
	if control.congestionWindowBytes < control.slowStartThresholdBytes {
		state.Phase = "slowStart"
	}
	return state
}

// bytesInFlight counts sent payload that has not yet been cumulatively ACKed.
// SYN and FIN reserve sequence positions but do not count as payload in flight.
func (c *Client) bytesInFlight() uint64 {
	if !c.handshakeCompleted || c.closing || c.terminated {
		return 0
	}
	return c.nextSendSeq - c.oldestUnackedSeq
}

// congestionCredit allows new bytes only while the window exceeds current flight.
// After a timeout, old data can exceed the reduced window and block new sends.
func (c *Client) congestionCredit() uint64 {
	if c.congestion.config.Mode == model.CongestionNone {
		return model.MaxCongestionWindowBytes
	}
	window := c.congestion.congestionWindowBytes
	flight := c.bytesInFlight()
	if flight >= window {
		return 0
	}
	return window - flight
}

// acknowledge grows Reno once for this ACK, using only newly confirmed bytes.
// A cumulative 4096-byte ACK grows slow start by one MSS, rather than four.
func (control *congestionControl) acknowledge(newBytes, ack uint64) {
	if control.timedOutSegmentEnd != 0 && ack >= control.timedOutSegmentEnd {
		control.timedOutSegmentEnd = 0
	}
	if control.config.Mode == model.CongestionNone {
		return
	}

	// RFC 5681 section 3.1: one ACK belongs to the phase active before growth.
	window := control.congestionWindowBytes
	if window < control.slowStartThresholdBytes {
		increase := min(newBytes, uint64(model.SEGMENT_SIZE))
		control.congestionWindowBytes = addCongestionBytes(window, increase)
		return
	}

	// Count a window of new ACKed bytes before adding one MSS in avoidance.
	acknowledgedBytes := addCongestionBytes(control.avoidanceAcknowledgedBytes, newBytes)
	control.avoidanceAcknowledgedBytes = acknowledgedBytes
	if acknowledgedBytes >= window {
		control.avoidanceAcknowledgedBytes -= window
		control.congestionWindowBytes = addCongestionBytes(window, model.SEGMENT_SIZE)
	}
}

// onDataTimeout halves flight once for this original segment, then restarts at one MSS.
// Retrying [513,1025) after a partial ACK of [1,1025) must not halve the threshold again.
func (control *congestionControl) onDataTimeout(packet model.Packet, flight uint64) {
	if control.config.Mode == model.CongestionNone {
		return
	}
	if control.timedOutSegmentEnd == 0 {
		control.slowStartThresholdBytes = max(flight/2, 2*model.SEGMENT_SIZE)
		control.timedOutSegmentEnd = packet.Seq + uint64(packet.Size)
	}
	control.congestionWindowBytes = model.SEGMENT_SIZE
	control.avoidanceAcknowledgedBytes = 0
}

// afterSYNRetry starts data with one MSS when the handshake already encountered loss.
func (control *congestionControl) afterSYNRetry() {
	if control.config.Mode == model.CongestionReno {
		// This connection's reduced initial window also applies to later idle restarts.
		control.startupWindowBytes = model.SEGMENT_SIZE
		control.congestionWindowBytes = model.SEGMENT_SIZE
		control.avoidanceAcknowledgedBytes = 0
	}
}

// restartAfterIdle avoids sending a previously enlarged window as a sudden burst.
// RFC 5681 section 4.1 restarts at min(current window, initial window) after an RTO of idle.
func (control *congestionControl) restartAfterIdle(now, currentRTO time.Duration) {
	if control.config.Mode == model.CongestionNone || !control.hasSentData {
		return
	}
	if now-control.lastDataSentAt > currentRTO {
		control.congestionWindowBytes = min(control.congestionWindowBytes, control.startupWindowBytes)
		control.avoidanceAcknowledgedBytes = 0
	}
}

// recordDataSent includes retransmissions, which also end an idle period.
func (control *congestionControl) recordDataSent(now time.Duration) {
	control.lastDataSentAt = now
	control.hasSentData = true
}

// addCongestionBytes saturates a window or ACK counter instead of wrapping it.
func addCongestionBytes(current, increase uint64) uint64 {
	if increase >= model.MaxCongestionWindowBytes-current {
		return model.MaxCongestionWindowBytes
	}
	return current + increase
}
