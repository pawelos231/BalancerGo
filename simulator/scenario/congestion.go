package scenario

import (
	"fmt"
	"math"

	"load-balancer/model"
)

// normalizeCongestion fills omitted fields and keeps byte-window arithmetic in range.
func normalizeCongestion(config CongestionConfig) (NormalizedCongestionConfig, error) {
	defaults := model.DefaultCongestionConfig()
	resolved := NormalizedCongestionConfig{
		Mode:                           string(defaults.Mode),
		CubicFastConvergence:           defaults.CubicFastConvergence,
		InitialWindowSegments:          defaults.InitialWindowSegments,
		InitialSlowStartThresholdBytes: defaults.InitialSlowStartThresholdBytes,
	}
	if config.Mode != nil {
		resolved.Mode = *config.Mode
	}
	if config.CubicFastConvergence != nil {
		resolved.CubicFastConvergence = *config.CubicFastConvergence
	}
	if config.InitialWindowSegments != nil {
		resolved.InitialWindowSegments = *config.InitialWindowSegments
	}
	if config.MaximumWindowSegments != nil {
		resolved.MaximumWindowSegments = *config.MaximumWindowSegments
	}
	if config.InitialSlowStartThresholdBytes != nil {
		resolved.InitialSlowStartThresholdBytes = *config.InitialSlowStartThresholdBytes
	}

	if !model.CongestionMode(resolved.Mode).Valid() {
		return NormalizedCongestionConfig{}, fmt.Errorf(
			"tcp.congestionControl.mode: must be none, reno, newReno, tahoe or cubic",
		)
	}

	// Each enabled algorithm can detect losses early and choose its own window response.
	resolved.FastRetransmit = resolved.Mode != string(model.CongestionNone) && defaults.FastRetransmit
	if config.FastRetransmit != nil {
		resolved.FastRetransmit = *config.FastRetransmit
	}
	if resolved.FastRetransmit && resolved.Mode == string(model.CongestionNone) {
		return NormalizedCongestionConfig{}, fmt.Errorf(
			"tcp.congestionControl.fastRetransmit: " +
				"requires Reno, NewReno, Tahoe or CUBIC congestion control",
		)
	}

	// Multiplying the segment count by 1,024 must still fit the sender's byte counters.
	maximumSegments := math.MaxInt64 / model.SEGMENT_SIZE
	if resolved.MaximumWindowSegments < 0 || resolved.MaximumWindowSegments > maximumSegments {
		return NormalizedCongestionConfig{}, fmt.Errorf(
			"tcp.congestionControl.maximumWindowSegments: must be in 0..%d; zero disables the cap",
			maximumSegments,
		)
	}
	if resolved.MaximumWindowSegments != 0 && resolved.Mode == string(model.CongestionNone) {
		return NormalizedCongestionConfig{}, fmt.Errorf(
			"tcp.congestionControl.maximumWindowSegments: requires congestion control",
		)
	}
	if resolved.InitialWindowSegments < 1 || resolved.InitialWindowSegments > maximumSegments {
		return NormalizedCongestionConfig{}, fmt.Errorf(
			"tcp.congestionControl.initialWindowSegments: must be in 1..%d",
			maximumSegments,
		)
	}
	if resolved.InitialSlowStartThresholdBytes < 2*model.SEGMENT_SIZE ||
		resolved.InitialSlowStartThresholdBytes > math.MaxInt64 {
		return NormalizedCongestionConfig{}, fmt.Errorf(
			"tcp.congestionControl.initialSlowStartThresholdBytes: must be in %d..%d",
			2*model.SEGMENT_SIZE,
			int64(math.MaxInt64),
		)
	}
	return resolved, nil
}
