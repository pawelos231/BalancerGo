package model

import "time"

// RTOMode selects a fixed timeout or one estimated from observed round trips.
type RTOMode string

const (
	RTOFixed    RTOMode = "fixed"
	RTOAdaptive RTOMode = "adaptive"
)

// RTOConfig controls timeout estimation separately from exponential backoff.
// Fixed mode with backoff disabled repeats the same timeout after every loss.
type RTOConfig struct {
	Mode               RTOMode
	ExponentialBackoff bool
	InitialTimeout     time.Duration
	MinimumTimeout     time.Duration
	MaximumTimeout     time.Duration
}

// DefaultRTOConfig starts at 1 s and bounds estimated/backed-off timeouts to 200 ms..60 s.
func DefaultRTOConfig() RTOConfig {
	return RTOConfig{
		Mode:               RTOAdaptive,
		ExponentialBackoff: true,
		InitialTimeout:     time.Second,
		MinimumTimeout:     200 * time.Millisecond,
		MaximumTimeout:     time.Minute,
	}
}

// RTOState keeps the latest estimates even after the connection finishes.
// Samples=0 means no unambiguous RTT has been measured, rather than a measured zero.
type RTOState struct {
	LatestRTT    time.Duration
	SmoothedRTT  time.Duration
	RTTVariation time.Duration
	BaseRTO      time.Duration
	CurrentRTO   time.Duration
	Samples      uint64
}
