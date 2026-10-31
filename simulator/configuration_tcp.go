package main

import (
	"errors"
	"flag"
	"time"

	"load-balancer/model"
)

// registerTCPFlags keeps congestion, retry timing and idle cleanup configurable.
func (c *configuration) registerTCPFlags() {
	c.rto = model.DefaultRTOConfig()
	c.congestion = model.DefaultCongestionConfig()
	clientIdle, halfOpenIdle, establishedIdle := c.effectiveIdleTimeouts()
	c.clientIdleTimeout = &clientIdle
	c.lbHalfOpenIdleTimeout = &halfOpenIdle
	c.lbEstablishedIdleTimeout = &establishedIdle

	flag.Func(
		"tcp-congestion-control",
		"TCP congestion control: none or reno (default reno)",
		func(value string) error {
			if value != string(model.CongestionNone) && value != string(model.CongestionReno) {
				return errors.New("tcp.congestionControl.mode: must be none or reno")
			}
			c.congestion.Mode = model.CongestionMode(value)
			return nil
		},
	)
	flag.IntVar(
		&c.congestion.InitialWindowSegments,
		"tcp-initial-window",
		c.congestion.InitialWindowSegments,
		"initial TCP congestion window in 1024-byte segments",
	)
	flag.Uint64Var(
		&c.congestion.InitialSlowStartThresholdBytes,
		"tcp-slow-start-threshold",
		c.congestion.InitialSlowStartThresholdBytes,
		"initial TCP slow-start threshold in bytes (at least 2048)",
	)

	flag.Func(
		"tcp-rto-mode",
		"TCP retransmission timing: fixed or adaptive (default adaptive)",
		func(value string) error {
			if value != string(model.RTOFixed) && value != string(model.RTOAdaptive) {
				return errors.New("tcp.rto.mode: must be fixed or adaptive")
			}
			c.rto.Mode = model.RTOMode(value)
			return nil
		},
	)
	flag.BoolVar(
		&c.rto.ExponentialBackoff,
		"tcp-rto-backoff",
		c.rto.ExponentialBackoff,
		"double the retransmission delay after a timeout",
	)
	flag.DurationVar(
		&c.rto.InitialTimeout,
		"tcp-rto-initial",
		c.rto.InitialTimeout,
		"initial virtual TCP retransmission timeout",
	)
	flag.DurationVar(
		&c.rto.MinimumTimeout,
		"tcp-rto-minimum",
		c.rto.MinimumTimeout,
		"minimum virtual TCP retransmission timeout",
	)
	flag.DurationVar(
		&c.rto.MaximumTimeout,
		"tcp-rto-maximum",
		c.rto.MaximumTimeout,
		"maximum virtual TCP retransmission timeout",
	)
	flag.DurationVar(
		c.clientIdleTimeout,
		"tcp-idle-timeout",
		clientIdle,
		"client idle timeout; 0 disables idle cleanup",
	)
	flag.DurationVar(
		c.lbHalfOpenIdleTimeout,
		"lb-half-open-idle-timeout",
		halfOpenIdle,
		"LB half-open idle timeout; 0 disables cleanup",
	)
	flag.DurationVar(
		c.lbEstablishedIdleTimeout,
		"lb-established-idle-timeout",
		establishedIdle,
		"LB established idle timeout; 0 disables cleanup",
	)
}

// effectiveRTOConfig keeps older internal configurations on the shared TCP defaults.
// A supplied mode marks a complete policy, including an explicit false backoff.
func (c configuration) effectiveRTOConfig() model.RTOConfig {
	if c.rto.Mode == "" {
		return model.DefaultRTOConfig()
	}
	return c.rto
}

// effectiveCongestionConfig gives older internal configurations the shared defaults.
// The explicit mode none preserves the earlier receive-window-only sender limit.
func (c configuration) effectiveCongestionConfig() model.CongestionConfig {
	if c.congestion.Mode == "" {
		return model.DefaultCongestionConfig()
	}
	return c.congestion
}

// effectiveIdleTimeouts distinguishes omitted limits from explicitly disabled zeroes.
func (c configuration) effectiveIdleTimeouts() (client, halfOpen, established time.Duration) {
	client = model.DefaultClientIdleTimeout
	halfOpen = model.DefaultLBHalfOpenIdleTimeout
	established = model.DefaultLBEstablishedIdleTimeout
	if c.clientIdleTimeout != nil {
		client = *c.clientIdleTimeout
	}
	if c.lbHalfOpenIdleTimeout != nil {
		halfOpen = *c.lbHalfOpenIdleTimeout
	}
	if c.lbEstablishedIdleTimeout != nil {
		established = *c.lbEstablishedIdleTimeout
	}
	return
}
