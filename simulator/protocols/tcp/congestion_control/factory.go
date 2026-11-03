package congestioncontrol

import "load-balancer/model"

// New selects one congestion-control algorithm before a connection starts.
// Invalid settings panic before a transport connection starts.
func New(config model.CongestionConfig) Policy {
	// Accept only the supported congestion-control modes.
	validMode := config.Mode.Valid()

	// Start with at least one segment, without exceeding the byte limit.
	maximumInitialSegments := model.MaxCongestionWindowBytes / model.SEGMENT_SIZE
	validInitialWindow := config.InitialWindowSegments >= 1 &&
		uint64(config.InitialWindowSegments) <= maximumInitialSegments

	// Keep the slow-start threshold between two segments and the byte limit.
	minimumThresholdBytes := uint64(2 * model.SEGMENT_SIZE)
	validThreshold := config.InitialSlowStartThresholdBytes >= minimumThresholdBytes &&
		config.InitialSlowStartThresholdBytes <= model.MaxCongestionWindowBytes

	// Mode none must leave fast retransmit disabled.
	validRetransmit := !config.FastRetransmit ||
		config.Mode != model.CongestionNone

	if !validMode || !validInitialWindow || !validThreshold || !validRetransmit {
		panic("tcp: invalid congestion configuration")
	}

	switch config.Mode {
	case model.CongestionReno:
		return &renoPolicy{classicWindow: newClassicWindow(config)}
	case model.CongestionNewReno:
		return newNewRenoPolicy(config)
	case model.CongestionTahoe:
		return &tahoePolicy{classicWindow: newClassicWindow(config)}
	case model.CongestionCubic:
		return newCubicPolicy(config)
	default:
		return unlimitedPolicy{}
	}
}
