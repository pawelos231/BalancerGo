package model

import (
	"math"
	"time"
)

// CloseConfig bounds waiting in virtual time; TIME_WAIT lasts twice MSL.
// The default 30-second MSL is a chosen simulator profile, not a measured packet lifetime.
type CloseConfig struct {
	MSL            time.Duration // Maximum Segment Lifetime: the assumed maximum time a segment can remain in the network.
	FinWaitTimeout time.Duration
	MaxFINRetries  int
}

func DefaultCloseConfig() CloseConfig {
	return CloseConfig{MSL: 30 * time.Second, FinWaitTimeout: time.Minute, MaxFINRetries: 5}
}

// Valid checks timers and FIN retries.
func (config CloseConfig) Valid() bool {
	// Positive timers; twice MSL must fit.
	validTimers := config.MSL > 0 && config.MSL <= time.Duration(math.MaxInt64/2) &&
		config.FinWaitTimeout > 0

	// Allow zero to 30 FIN retries.
	validRetries := config.MaxFINRetries >= 0 && config.MaxFINRetries <= 30

	return validTimers && validRetries
}

type CloseOutcome string

const (
	CloseGraceful      CloseOutcome = "graceful"
	CloseReset         CloseOutcome = "reset"
	CloseFINRetryLimit CloseOutcome = "finRetryLimit"
	CloseWaitTimeout   CloseOutcome = "finWaitTimeout"
	CloseAborted       CloseOutcome = "aborted"
)

// CloseState describes transport teardown separately from successful payload delivery.
// An empty Outcome means the connection has not reached its terminal close result yet.
type CloseState struct {
	State              TCPState
	Outcome            CloseOutcome
	FINRetransmissions uint64
	Elapsed            time.Duration
}
