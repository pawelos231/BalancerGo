package scenario

import (
	"fmt"
	"math"
	"time"

	"load-balancer/model"
)

// normalizeTCP fills omitted settings without treating explicit zero as a default.
func normalizeTCP(config TCPConfig) (NormalizedTCPConfig, error) {
	capacity := model.DefaultReceiveBufferBytes
	if config.ReceiveBufferBytes != nil {
		capacity = *config.ReceiveBufferBytes
	}
	if capacity < 1 || capacity > math.MaxUint16 {
		return NormalizedTCPConfig{}, fmt.Errorf("tcp.receiveBufferBytes: must be in 1..65535")
	}

	rto, err := normalizeRTO(config.RTO)
	if err != nil {
		return NormalizedTCPConfig{}, err
	}
	congestion, err := normalizeCongestion(config.CongestionControl)
	if err != nil {
		return NormalizedTCPConfig{}, err
	}
	idleTimeout, err := normalizeIdleTimeout(
		config.ClientIdleTimeout,
		model.DefaultClientIdleTimeout,
		"tcp.clientIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedTCPConfig{}, err
	}

	return NormalizedTCPConfig{
		ReceiveBufferBytes: capacity,
		RTO:                rto,
		CongestionControl:  congestion,
		ClientIdleTimeout:  idleTimeout,
	}, nil
}

// normalizeRTO keeps timer limits positive and ordered: minimum <= initial <= maximum.
func normalizeRTO(config RTOConfig) (NormalizedRTOConfig, error) {
	defaults := model.DefaultRTOConfig()
	resolved := NormalizedRTOConfig{
		Mode:               string(defaults.Mode),
		ExponentialBackoff: defaults.ExponentialBackoff,
		InitialTimeout:     defaults.InitialTimeout,
		MinimumTimeout:     defaults.MinimumTimeout,
		MaximumTimeout:     defaults.MaximumTimeout,
	}
	if config.Mode != nil {
		resolved.Mode = *config.Mode
	}
	if config.ExponentialBackoff != nil {
		resolved.ExponentialBackoff = *config.ExponentialBackoff
	}
	if config.InitialTimeout != nil {
		resolved.InitialTimeout = *config.InitialTimeout
	}
	if config.MinimumTimeout != nil {
		resolved.MinimumTimeout = *config.MinimumTimeout
	}
	if config.MaximumTimeout != nil {
		resolved.MaximumTimeout = *config.MaximumTimeout
	}

	if resolved.Mode != string(model.RTOFixed) && resolved.Mode != string(model.RTOAdaptive) {
		return NormalizedRTOConfig{}, fmt.Errorf("tcp.rto.mode: must be fixed or adaptive")
	}
	nonpositiveTimeout := resolved.InitialTimeout <= 0 ||
		resolved.MinimumTimeout <= 0 || resolved.MaximumTimeout <= 0
	if nonpositiveTimeout {
		return NormalizedRTOConfig{}, fmt.Errorf("tcp.rto: initial, minimum and maximum timeouts must be positive")
	}
	limitsOutOfOrder := resolved.MinimumTimeout > resolved.InitialTimeout ||
		resolved.InitialTimeout > resolved.MaximumTimeout
	if limitsOutOfOrder {
		return NormalizedRTOConfig{}, fmt.Errorf("tcp.rto: require minimum <= initial <= maximum timeout")
	}
	return resolved, nil
}

// normalizeIdleTimeout permits zero to disable cleanup while rejecting negative delays.
func normalizeIdleTimeout(
	value *time.Duration,
	fallback time.Duration,
	path string,
) (time.Duration, error) {
	if value == nil {
		return fallback, nil
	}
	if *value < 0 {
		return 0, fmt.Errorf("%s: must be nonnegative; zero disables idle cleanup", path)
	}
	return *value, nil
}

// normalizeLoadBalancer resolves separate idle limits for pending and established connections.
func normalizeLoadBalancer(config LoadBalancerConfig) (NormalizedLoadBalancerConfig, error) {
	if err := validateLoadBalancer(config); err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	halfOpen, err := normalizeIdleTimeout(
		config.HalfOpenIdleTimeout,
		model.DefaultLBHalfOpenIdleTimeout,
		"resources.loadBalancer.halfOpenIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	established, err := normalizeIdleTimeout(
		config.EstablishedIdleTimeout,
		model.DefaultLBEstablishedIdleTimeout,
		"resources.loadBalancer.establishedIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	return NormalizedLoadBalancerConfig{
		ID:                      config.ID,
		Algorithm:               config.Algorithm,
		Ingress:                 config.Ingress,
		ForwardingBitsPerSecond: config.ForwardingBitsPerSecond,
		HalfOpenIdleTimeout:     halfOpen,
		EstablishedIdleTimeout:  established,
	}, nil
}
