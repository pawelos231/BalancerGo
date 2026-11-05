package congestioncontrol

import "load-balancer/model"

// New selects one congestion-control algorithm before a connection starts.
// Invalid settings panic before a transport connection starts.
func New(config model.CongestionConfig) Policy {
	return NewWithMSS(config, model.DefaultMSSBytes)
}

// NewWithMSS uses the connection's payload limit for window growth and recovery.
// For example, four segments at MSS=1460 start with a 5840-byte window.
func NewWithMSS(config model.CongestionConfig, mssBytes int) Policy {
	if !model.ValidMSSBytes(mssBytes) {
		panic("tcp: MSS is outside the supported IPv4 payload range")
	}
	segmentBytes := uint64(mssBytes)

	// Accept only the supported congestion-control modes.
	validMode := config.Mode.Valid()

	// Start with at least one segment, without exceeding the byte limit.
	maximumInitialSegments := model.MaxCongestionWindowBytes / segmentBytes
	validInitialWindow := config.InitialWindowSegments >= 1 &&
		uint64(config.InitialWindowSegments) <= maximumInitialSegments

	// Bound the actual window, including recovery inflation, when a ceiling is set.
	validMaximumWindow := config.MaximumWindowSegments >= 0 &&
		uint64(config.MaximumWindowSegments) <= maximumInitialSegments
	if config.Mode == model.CongestionNone && config.MaximumWindowSegments != 0 {
		validMaximumWindow = false
	}

	// Keep the slow-start threshold between two segments and the byte limit.
	minimumThresholdBytes := 2 * segmentBytes
	validThreshold := config.InitialSlowStartThresholdBytes >= minimumThresholdBytes &&
		config.InitialSlowStartThresholdBytes <= model.MaxCongestionWindowBytes

	// Mode none must leave fast retransmit disabled.
	validRetransmit := !config.FastRetransmit ||
		config.Mode != model.CongestionNone

	if !validMode || !validInitialWindow || !validMaximumWindow || !validThreshold || !validRetransmit {
		panic("tcp: invalid congestion configuration")
	}

	switch config.Mode {
	case model.CongestionReno:
		return &renoPolicy{classicWindow: newClassicWindow(config, mssBytes)}
	case model.CongestionNewReno:
		return newNewRenoPolicy(config, mssBytes)
	case model.CongestionTahoe:
		return &tahoePolicy{classicWindow: newClassicWindow(config, mssBytes)}
	case model.CongestionCubic:
		return newCubicPolicy(config, mssBytes)
	default:
		return unlimitedPolicy{}
	}
}
