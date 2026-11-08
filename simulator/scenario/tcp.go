package scenario

import (
	"fmt"
	"math"
	"time"

	"load-balancer/model"
)

// normalizeTCP fills omitted settings without treating explicit zero as a default.
func normalizeTCP(config TCPConfig) (NormalizedTCPConfig, error) {
	mssBytes := model.DefaultMSSBytes
	if config.MSSBytes != nil {
		mssBytes = *config.MSSBytes
	}
	if !model.ValidMSSBytes(mssBytes) {
		return NormalizedTCPConfig{}, fmt.Errorf("tcp.mssBytes: must be in 1..%d", model.MaxMSSBytes)
	}

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
	congestion, err := normalizeCongestion(config.CongestionControl, mssBytes)
	if err != nil {
		return NormalizedTCPConfig{}, err
	}
	delayedACK, err := normalizeDelayedACK(config.DelayedACK)
	if err != nil {
		return NormalizedTCPConfig{}, err
	}
	ecnEnabled := config.ECNEnabled != nil && *config.ECNEnabled
	if ecnEnabled && congestion.Mode == string(model.CongestionNone) {
		return NormalizedTCPConfig{}, fmt.Errorf("tcp.ECNenabled: requires congestion control")
	}

	// SACK recovery replaces classic recovery only for the supported profiles.
	sackEnabled := config.SACKEnabled != nil && *config.SACKEnabled
	if sackEnabled {
		supportedMode := congestion.Mode == string(model.CongestionReno) ||
			congestion.Mode == string(model.CongestionNewReno) ||
			congestion.Mode == string(model.CongestionCubic)
		if !supportedMode || !congestion.FastRetransmit {
			return NormalizedTCPConfig{}, fmt.Errorf(
				"tcp.sackEnabled: requires Reno, NewReno or CUBIC with fastRetransmit enabled",
			)
		}
	}

	closeConfig, err := normalizeClose(config.Close)
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

	backendHalfOpen, err := normalizeIdleTimeout(
		config.BackendHalfOpenIdleTimeout,
		model.DefaultBackendHalfOpenIdleTimeout,
		"tcp.backendHalfOpenIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedTCPConfig{}, err
	}

	backendEstablished, err := normalizeIdleTimeout(
		config.BackendEstablishedIdleTimeout,
		model.DefaultBackendEstablishedIdleTimeout,
		"tcp.backendEstablishedIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedTCPConfig{}, err
	}

	return NormalizedTCPConfig{
		MSSBytes:                      mssBytes,
		ReceiveBufferBytes:            capacity,
		SACKEnabled:                   sackEnabled,
		ECNEnabled:                    ecnEnabled,
		DelayedACK:                    delayedACK,
		RTO:                           rto,
		Close:                         closeConfig,
		CongestionControl:             congestion,
		ClientIdleTimeout:             idleTimeout,
		BackendHalfOpenIdleTimeout:    backendHalfOpen,
		BackendEstablishedIdleTimeout: backendEstablished,
	}, nil
}

// normalizeClose validates twice-MSL arithmetic before shutdown schedules timers.
func normalizeClose(config CloseConfig) (NormalizedCloseConfig, error) {
	defaults := model.DefaultCloseConfig()
	resolved := NormalizedCloseConfig{
		MSL: defaults.MSL, FinWaitTimeout: defaults.FinWaitTimeout, MaxFINRetries: defaults.MaxFINRetries,
	}
	if config.MSL != nil {
		resolved.MSL = *config.MSL
	}
	if config.FinWaitTimeout != nil {
		resolved.FinWaitTimeout = *config.FinWaitTimeout
	}
	if config.MaxFINRetries != nil {
		resolved.MaxFINRetries = *config.MaxFINRetries
	}
	if resolved.MSL <= 0 || resolved.MSL > time.Duration(math.MaxInt64/2) {
		return NormalizedCloseConfig{}, fmt.Errorf("tcp.close.mslNanoseconds: must be positive and at most half the time.Duration range")
	}
	if resolved.FinWaitTimeout <= 0 {
		return NormalizedCloseConfig{}, fmt.Errorf("tcp.close.finWaitTimeoutNanoseconds: must be positive")
	}
	if resolved.MaxFINRetries < 0 || resolved.MaxFINRetries > 30 {
		return NormalizedCloseConfig{}, fmt.Errorf("tcp.close.maxFINRetries: must be in 0..30")
	}
	return resolved, nil
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

// normalizeLoadBalancer resolves an independent idle limit for each mapping phase.
func normalizeLoadBalancer(
	config LoadBalancerConfig,
	duration time.Duration,
	path, timeoutPath string,
) (NormalizedLoadBalancerConfig, error) {
	if err := validateLoadBalancer(config, path); err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	host, err := normalizeHost(config.Host, duration, path+".host")
	if err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	halfOpen, err := normalizeIdleTimeout(
		config.HalfOpenIdleTimeout,
		model.DefaultLBHalfOpenIdleTimeout,
		timeoutPath+".halfOpenIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	established, err := normalizeIdleTimeout(
		config.EstablishedIdleTimeout,
		model.DefaultLBEstablishedIdleTimeout,
		timeoutPath+".establishedIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	closing, err := normalizeIdleTimeout(
		config.ClosingIdleTimeout,
		model.DefaultLBClosingIdleTimeout,
		timeoutPath+".closingIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	reset, err := normalizeIdleTimeout(
		config.ResetIdleTimeout,
		model.DefaultLBResetIdleTimeout,
		timeoutPath+".resetIdleTimeoutNanoseconds",
	)
	if err != nil {
		return NormalizedLoadBalancerConfig{}, err
	}
	return NormalizedLoadBalancerConfig{
		ID:                      config.ID,
		Algorithm:               config.Algorithm,
		Host:                    host,
		Ingress:                 config.Ingress,
		ForwardingBitsPerSecond: config.ForwardingBitsPerSecond,
		HalfOpenIdleTimeout:     halfOpen,
		EstablishedIdleTimeout:  established,
		ClosingIdleTimeout:      closing,
		ResetIdleTimeout:        reset,
	}, nil
}
