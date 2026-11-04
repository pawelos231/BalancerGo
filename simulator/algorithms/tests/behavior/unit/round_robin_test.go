package algorithms_test

import (
	"testing"

	"load-balancer/algorithms"
)

func TestRoundRobinPreservesRotationAcrossCapacityAndListChanges(t *testing.T) {
	available := algorithms.Candidate{}
	full := algorithms.Candidate{MaxConnections: 1, MappedConnections: 1}
	initial := []algorithms.Candidate{available, full, available, full}
	allFull := []algorithms.Candidate{full, full, full, full}
	shorter := []algorithms.Candidate{available, available}
	longer := []algorithms.Candidate{full, available, available}

	algorithm := &algorithms.RoundRobin{}
	for _, step := range []struct {
		name       string
		candidates []algorithms.Candidate
		want       int
	}{
		{"first candidate", initial, 0},
		{"skip full candidate", initial, 2},
		{"full rotation fails", allFull, -1},
		{"empty list preserves cursor", nil, -1},
		{"shrink normalizes cursor", shorter, 1},
		{"grow keeps cursor", longer, 1},
		{"next candidate", longer, 2},
		{"wrap and skip full candidate", longer, 1},
		{"full singleton fails", []algorithms.Candidate{full}, -1},
		{"grow after singleton", []algorithms.Candidate{available, available, available}, 0},
	} {
		t.Run(step.name, func(t *testing.T) {
			got, err := algorithm.Pick(step.candidates)
			if got != step.want || (err != nil) != (step.want == -1) {
				t.Fatalf("Pick() = %d, %v; want index %d", got, err, step.want)
			}
		})
	}
}
