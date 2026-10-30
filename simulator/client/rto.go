package client

import (
	"time"

	"load-balancer/model"
)

// rtoEstimator keeps a measured timeout separate from its loss-related backoff.
// For example, a 1 s base can temporarily become 2 s after a timeout.
type rtoEstimator struct {
	config model.RTOConfig
	state  model.RTOState
}

// newRTOEstimator starts with the configured timeout, before any RTT is known.
func newRTOEstimator(config model.RTOConfig) rtoEstimator {
	validMode := config.Mode == model.RTOFixed || config.Mode == model.RTOAdaptive
	validLimits := config.MinimumTimeout > 0 &&
		config.MinimumTimeout <= config.InitialTimeout &&
		config.InitialTimeout <= config.MaximumTimeout
	if !validMode || !validLimits {
		panic("client: invalid RTO configuration")
	}

	return rtoEstimator{
		config: config,
		state: model.RTOState{
			BaseRTO:    config.InitialTimeout,
			CurrentRTO: config.InitialTimeout,
		},
	}
}

// observeRTT smooths a safe measurement and removes any previous backoff.
// A first 400 ms sample gives SRTT=400 ms, variation=200 ms and raw RTO=1.2 s.
func (estimator *rtoEstimator) observeRTT(sample time.Duration) {
	if sample < 0 {
		panic("client: RTT sample must not be negative")
	}

	state := &estimator.state
	if state.Samples == 0 {
		state.SmoothedRTT = sample
		state.RTTVariation = sample / 2
	} else {
		// Variation uses the previous smoothed RTT, before that estimate changes.
		difference := state.SmoothedRTT - sample
		if difference < 0 {
			difference = -difference
		}
		state.RTTVariation = smoothDuration(state.RTTVariation, difference, 4)
		state.SmoothedRTT = smoothDuration(state.SmoothedRTT, sample, 8)
	}
	state.LatestRTT = sample
	state.Samples++

	state.BaseRTO = estimator.config.InitialTimeout
	if estimator.config.Mode == model.RTOAdaptive {
		state.BaseRTO = estimator.measuredTimeout()
	}
	state.CurrentRTO = state.BaseRTO
}

// smoothDuration moves partway toward a sample without overflowing a weighted sum.
// With weight 8, 400 ms moves to 425 ms when the new sample is 600 ms.
func smoothDuration(previous, sample time.Duration, weight int64) time.Duration {
	divisor := time.Duration(weight)
	change := sample - previous
	weightedChange := change / divisor

	// Round down to whole nanoseconds, including when the estimate decreases.
	if change < 0 && change%divisor != 0 {
		weightedChange--
	}
	return previous + weightedChange
}

// measuredTimeout adds a variation margin, then applies the configured limits.
// SRTT=400 ms and variation=200 ms give RTO=400+4*200=1200 ms.
// With limits 200 ms..60 s, a computed 100 ms becomes 200 ms, and 70 s becomes 60 s.
func (estimator *rtoEstimator) measuredTimeout() time.Duration {
	maximum := estimator.config.MaximumTimeout
	variation := estimator.state.RTTVariation
	if variation > maximum/4 {
		return maximum
	}

	// RFC 6298, sections 2.2-2.3: RTO = SRTT + max(G, 4*RTTVAR).
	// Our virtual clock uses G=1 ns: https://www.rfc-editor.org/rfc/rfc6298.html#section-2
	margin := max(time.Nanosecond, 4*variation)
	smoothed := estimator.state.SmoothedRTT
	if smoothed >= maximum || margin >= maximum-smoothed {
		return maximum
	}
	return max(estimator.config.MinimumTimeout, smoothed+margin)
}

// backOff doubles the effective timeout, saturating at the configured maximum.
// Starting at 1 s produces waits of 2 s, 4 s and 8 s after consecutive timeouts.
func (estimator *rtoEstimator) backOff() {
	if !estimator.config.ExponentialBackoff {
		return
	}

	current := estimator.state.CurrentRTO
	maximum := estimator.config.MaximumTimeout
	if current >= maximum-current {
		estimator.state.CurrentRTO = maximum
		return
	}
	estimator.state.CurrentRTO = current * 2
}

// afterSYNRetry keeps adaptive data conservative after an ambiguous handshake.
// Fixed mode retains its fixed base, so fixed/off can replay the earlier profile.
func (estimator *rtoEstimator) afterSYNRetry() {
	if estimator.config.Mode != model.RTOAdaptive {
		return
	}

	conservativeTimeout := min(3*time.Second, estimator.config.MaximumTimeout)
	estimator.state.CurrentRTO = max(estimator.state.CurrentRTO, conservativeTimeout)
}
