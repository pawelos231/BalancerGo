package presentation_test

import (
	"reflect"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
)

// TestPerformanceUsesWallTime keeps event and packet rates separate from virtual time.
func TestPerformanceUsesWallTime(t *testing.T) {
	got := presentation.MeasurePerformance(2*time.Second, 500, 100)
	want := &contract.SimulationPerformance{
		WallTimeSeconds:               2,
		EventsExecuted:                500,
		EventsPerWallSecond:           250,
		ForwardedPackets:              100,
		ForwardedPacketsPerWallSecond: 50,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("performance = %+v, want %+v", got, want)
	}
}

// TestPerformanceHandlesNonpositiveTime avoids dividing by zero before a clock advances.
func TestPerformanceHandlesNonpositiveTime(t *testing.T) {
	cases := []struct {
		name    string
		elapsed time.Duration
	}{
		{name: "zero", elapsed: 0},
		{name: "negative", elapsed: -time.Nanosecond},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := presentation.MeasurePerformance(test.elapsed, 500, 100)
			want := &contract.SimulationPerformance{
				EventsExecuted:   500,
				ForwardedPackets: 100,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("performance = %+v, want finite zero rates and retained counters", got)
			}
		})
	}
}
