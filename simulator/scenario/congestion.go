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
		InitialWindowSegments:          defaults.InitialWindowSegments,
		InitialSlowStartThresholdBytes: defaults.InitialSlowStartThresholdBytes,
	}
	if config.Mode != nil {
		resolved.Mode = *config.Mode
	}
	if config.InitialWindowSegments != nil {
		resolved.InitialWindowSegments = *config.InitialWindowSegments
	}
	if config.InitialSlowStartThresholdBytes != nil {
		resolved.InitialSlowStartThresholdBytes = *config.InitialSlowStartThresholdBytes
	}

	if resolved.Mode != string(model.CongestionNone) && resolved.Mode != string(model.CongestionReno) {
		return NormalizedCongestionConfig{}, fmt.Errorf("tcp.congestionControl.mode: must be none or reno")
	}

	// Multiplying the segment count by 1,024 must still fit the sender's byte counters.
	maximumSegments := math.MaxInt64 / model.SEGMENT_SIZE
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
