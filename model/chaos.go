package model

import "time"

// to be used later, not really used right now (also must be implemented on the frontend)
type ChaosConfig struct {
	Latency        time.Duration
	PacketDropRate float64
}
