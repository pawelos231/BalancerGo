package algorithms_test

import (
	"testing"

	"load-balancer/algorithms"
)

func TestSelectionRespectsMappingCapacity(t *testing.T) {
	for _, algorithm := range []struct {
		name         string
		newAlgorithm func() algorithms.Algorithm
	}{
		{"round robin", func() algorithms.Algorithm { return &algorithms.RoundRobin{} }},
		{"random", func() algorithms.Algorithm { return algorithms.NewRandom(7) }},
	} {
		t.Run(algorithm.name, func(t *testing.T) {
			for _, test := range []struct {
				name       string
				candidates []algorithms.Candidate
				want       int
			}{
				{"empty", nil, -1},
				{"unlimited", []algorithms.Candidate{{MappedConnections: 99}}, 0},
				{"below limit", []algorithms.Candidate{{MaxConnections: 2, MappedConnections: 1}}, 0},
				{"at limit", []algorithms.Candidate{{MaxConnections: 2, MappedConnections: 2}}, -1},
				{"over limit", []algorithms.Candidate{{MaxConnections: 2, MappedConnections: 3}}, -1},
			} {
				t.Run(test.name, func(t *testing.T) {
					got, err := algorithm.newAlgorithm().Pick(test.candidates)
					if got != test.want || (err != nil) != (test.want == -1) {
						t.Fatalf("Pick() = %d, %v; want index %d", got, err, test.want)
					}
				})
			}
		})
	}
}
