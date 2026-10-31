package model

// CongestionMode selects Reno's sender limit or the earlier rwnd-only baseline.
type CongestionMode string

const (
	CongestionNone CongestionMode = "none"
	CongestionReno CongestionMode = "reno"
)

// MaxCongestionWindowBytes keeps configuration and byte arithmetic within int64.
// Larger windows add no useful capacity to this model's unscaled receive window.
const MaxCongestionWindowBytes uint64 = 1<<63 - 1

// CongestionConfig controls the sender's initial window and slow-start threshold.
// Window size is configured in segments; the threshold counts payload bytes.
type CongestionConfig struct {
	Mode                           CongestionMode
	InitialWindowSegments          int
	InitialSlowStartThresholdBytes uint64
}

// DefaultCongestionConfig starts with four 1024-byte segments and a high threshold.
// RFC 5681, section 3.1: https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1
func DefaultCongestionConfig() CongestionConfig {
	return CongestionConfig{
		Mode:                           CongestionReno,
		InitialWindowSegments:          4,
		InitialSlowStartThresholdBytes: DefaultReceiveBufferBytes,
	}
}

// CongestionState describes the current sender limit, not network queue occupancy.
// BytesInFlight counts sent payload still awaiting a cumulative ACK.
type CongestionState struct {
	Mode                    CongestionMode
	CongestionWindowBytes   uint64
	SlowStartThresholdBytes uint64
	BytesInFlight           uint64
	Phase                   string
}
