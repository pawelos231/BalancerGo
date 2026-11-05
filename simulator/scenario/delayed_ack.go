package scenario

import (
	"fmt"
	"time"

	"load-balancer/model"
)

// normalizeDelayedACK preserves explicit values instead of replacing zero with a default.
func normalizeDelayedACK(config DelayedACKConfig) (NormalizedDelayedACKConfig, error) {
	defaults := model.DefaultDelayedACKConfig()
	resolved := NormalizedDelayedACKConfig{
		Enabled:        defaults.Enabled,
		SegmentsPerACK: defaults.SegmentsPerACK,
		MaxDelay:       defaults.MaxDelay,
	}
	if config.Enabled != nil {
		resolved.Enabled = *config.Enabled
	}
	if config.SegmentsPerACK != nil {
		resolved.SegmentsPerACK = *config.SegmentsPerACK
	}
	if config.MaxDelay != nil {
		resolved.MaxDelay = *config.MaxDelay
	}

	if resolved.SegmentsPerACK < 1 || resolved.SegmentsPerACK > 2 {
		return NormalizedDelayedACKConfig{}, fmt.Errorf("tcp.delayedAck.segmentsPerACK: must be in 1..2")
	}
	if resolved.MaxDelay <= 0 || resolved.MaxDelay > 500*time.Millisecond {
		return NormalizedDelayedACKConfig{}, fmt.Errorf(
			"tcp.delayedAck.maxDelayNanoseconds: must be positive and at most 500 ms",
		)
	}
	return resolved, nil
}
