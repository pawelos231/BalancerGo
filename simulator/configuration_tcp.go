package main

import (
	"errors"
	"flag"
	"time"

	"load-balancer/model"
)

// registerTCPFlags keeps congestion, retry timing and idle cleanup configurable.
func (c *configuration) registerTCPFlags() {
	flag.BoolVar(&c.sackEnabled, "sack-enabled", false,
		"negotiate selective ACKs; requires Reno, NewReno or CUBIC and fast retransmit")
	flag.BoolVar(&c.ecnEnabled, "ecn-enabled", false,
		"negotiate ECN congestion signaling; requires congestion control other than none")
	c.rto = model.DefaultRTOConfig()
	c.congestion = model.DefaultCongestionConfig()
	closeConfig := model.DefaultCloseConfig()
	c.closeConfig = &closeConfig
	clientIdle, halfOpenIdle, establishedIdle := c.effectiveIdleTimeouts()
	c.clientIdleTimeout = &clientIdle
	c.lbHalfOpenIdleTimeout = &halfOpenIdle
	c.lbEstablishedIdleTimeout = &establishedIdle
	closingIdle, resetIdle := c.effectiveLBFinalIdleTimeouts()
	c.lbClosingIdleTimeout = &closingIdle
	c.lbResetIdleTimeout = &resetIdle
	backendHalfOpen, backendEstablished := c.effectiveBackendIdleTimeouts()
	c.backendHalfOpenIdleTimeout = &backendHalfOpen
	c.backendEstablishedIdleTimeout = &backendEstablished
	flag.DurationVar(&c.closeConfig.MSL, "tcp-msl", closeConfig.MSL,
		"maximum segment lifetime; TIME_WAIT lasts twice this virtual duration")
	flag.DurationVar(&c.closeConfig.FinWaitTimeout, "tcp-fin-wait-timeout", closeConfig.FinWaitTimeout,
		"maximum virtual wait for the peer FIN after our FIN is acknowledged")
	flag.IntVar(&c.closeConfig.MaxFINRetries, "tcp-fin-retries", closeConfig.MaxFINRetries,
		"maximum FIN retransmissions after the first send (0..30)")

	flag.Func(
		"tcp-congestion-control",
		"TCP congestion control: none, reno, newReno, tahoe or cubic (default reno)",
		func(value string) error {
			if !model.CongestionMode(value).Valid() {
				return errors.New(
					"tcp.congestionControl.mode: must be none, reno, newReno, tahoe or cubic",
				)
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
	flag.BoolVar(
		&c.congestion.FastRetransmit,
		"tcp-fast-retransmit",
		c.congestion.FastRetransmit,
		"retry after three duplicate ACKs; the selected algorithm chooses its window response",
	)
	flag.BoolVar(
		&c.congestion.CubicFastConvergence,
		"tcp-cubic-fast-convergence",
		c.congestion.CubicFastConvergence,
		"enable CUBIC fast convergence; disable for experiments with one flow",
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
		c.backendHalfOpenIdleTimeout,
		"backend-half-open-idle-timeout",
		backendHalfOpen,
		"backend handshake deadline; 0 disables cleanup",
	)
	flag.DurationVar(
		c.backendEstablishedIdleTimeout,
		"backend-established-idle-timeout",
		backendEstablished,
		"backend connection idle timeout; 0 disables cleanup",
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
	flag.DurationVar(
		c.lbClosingIdleTimeout,
		"lb-closing-idle-timeout",
		closingIdle,
		"LB closing idle timeout; 0 disables cleanup",
	)
	flag.DurationVar(
		c.lbResetIdleTimeout,
		"lb-reset-idle-timeout",
		resetIdle,
		"LB reset idle timeout; 0 disables cleanup",
	)
}

// effectiveCloseConfig supplies shared defaults for older internal configurations.
func (c configuration) effectiveCloseConfig() model.CloseConfig {
	if c.closeConfig == nil {
		return model.DefaultCloseConfig()
	}
	return *c.closeConfig
}

// resolveCongestionFlagDefaults keeps -tcp-congestion-control none usable alone.
// An explicit -tcp-fast-retransmit=true still fails validation with mode none.
func (c *configuration) resolveCongestionFlagDefaults() {
	if c.congestion.Mode != model.CongestionNone {
		return
	}
	explicitFastRetransmit := false
	flag.Visit(func(option *flag.Flag) {
		if option.Name == "tcp-fast-retransmit" {
			explicitFastRetransmit = true
		}
	})
	if !explicitFastRetransmit {
		c.congestion.FastRetransmit = false
	}
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

// effectiveLBFinalIdleTimeouts keeps closing and reset limits independent of endpoint timers.
func (c configuration) effectiveLBFinalIdleTimeouts() (closing, reset time.Duration) {
	closing = model.DefaultLBClosingIdleTimeout
	reset = model.DefaultLBResetIdleTimeout
	if c.lbClosingIdleTimeout != nil {
		closing = *c.lbClosingIdleTimeout
	}
	if c.lbResetIdleTimeout != nil {
		reset = *c.lbResetIdleTimeout
	}
	return
}

// effectiveBackendIdleTimeouts keeps omitted settings distinct from disabled cleanup.
func (c configuration) effectiveBackendIdleTimeouts() (halfOpen, established time.Duration) {
	halfOpen = model.DefaultBackendHalfOpenIdleTimeout
	established = model.DefaultBackendEstablishedIdleTimeout

	if c.backendHalfOpenIdleTimeout != nil {
		halfOpen = *c.backendHalfOpenIdleTimeout
	}
	if c.backendEstablishedIdleTimeout != nil {
		established = *c.backendEstablishedIdleTimeout
	}
	return
}
