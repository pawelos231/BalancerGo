package main

import "time"

// simulationPerformance measures real execution, separately from virtual traffic.
// Forwarded packets count LB transmissions in both directions, including ACKs.
type simulationPerformance struct {
	WallTimeSeconds               float64 `json:"wallTimeSeconds"`
	EventsExecuted                uint64  `json:"eventsExecuted"`
	EventsPerWallSecond           float64 `json:"eventsPerWallSecond"`
	ForwardedPackets              uint64  `json:"forwardedPackets"`
	ForwardedPacketsPerWallSecond float64 `json:"forwardedPacketsPerWallSecond"`
}

// measurePerformance reports averages since run creation, including setup,
// snapshot work and any playback waits. Finished runs use their frozen duration.
func measurePerformance(elapsed time.Duration, events, packets uint64) *simulationPerformance {
	measurement := &simulationPerformance{
		EventsExecuted:   events,
		ForwardedPackets: packets,
	}
	if elapsed > 0 {
		measurement.WallTimeSeconds = elapsed.Seconds()
		measurement.EventsPerWallSecond = float64(events) / elapsed.Seconds()
		measurement.ForwardedPacketsPerWallSecond = float64(packets) / elapsed.Seconds()
	}
	return measurement
}
