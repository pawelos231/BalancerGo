package model

import "time"

// DelayedACKConfig lets the receiver confirm a pair of segments with one ACK.
// MaxDelay bounds the wait for the second segment in virtual time.
type DelayedACKConfig struct {
	Enabled        bool
	SegmentsPerACK int
	MaxDelay       time.Duration
}

func DefaultDelayedACKConfig() DelayedACKConfig {
	return DelayedACKConfig{
		SegmentsPerACK: 2,
		MaxDelay:       40 * time.Millisecond,
	}
}

// Valid keeps this profile at one ACK per one or two segments, within 500 ms.
// RFC 5681 section 4.2: https://www.rfc-editor.org/rfc/rfc5681.html#section-4.2
func (config DelayedACKConfig) Valid() bool {
	validThreshold := config.SegmentsPerACK >= 1 && config.SegmentsPerACK <= 2
	validDelay := config.MaxDelay > 0 && config.MaxDelay <= 500*time.Millisecond
	return validThreshold && validDelay
}

// ACKState counts standalone ACK packets; SYN/ACK and FIN/ACK are excluded.
// TimerTriggeredACKCount is the subset sent after waiting for a missing partner.
type ACKState struct {
	Enabled                bool
	SentACKCount           uint64
	TimerTriggeredACKCount uint64
}
