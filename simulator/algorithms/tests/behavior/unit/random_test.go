package algorithms_test

import (
	"math/rand"
	"testing"

	"load-balancer/algorithms"
)

func TestRandomPreservesSelectionOrderAndRNGSequence(t *testing.T) {
	available := algorithms.Candidate{}
	full := algorithms.Candidate{MaxConnections: 1, MappedConnections: 1}
	over := algorithms.Candidate{MaxConnections: 1, MappedConnections: 2}
	algorithm := algorithms.NewRandom(17)
	reference := rand.New(rand.NewSource(17))

	for _, step := range []struct {
		name       string
		candidates []algorithms.Candidate
		indices    []int
	}{
		{"empty", nil, nil},
		{"unavailable", []algorithms.Candidate{full, over}, nil},
		{"filtered indices", []algorithms.Candidate{full, available, over, available}, []int{1, 3}},
		{"singleton consumes a draw", []algorithms.Candidate{full, available, full}, []int{1}},
		{"unavailable after success", []algorithms.Candidate{over}, nil},
		{"three candidates", []algorithms.Candidate{available, available, full, available}, []int{0, 1, 3}},
	} {
		t.Run(step.name, func(t *testing.T) {
			want := -1
			if len(step.indices) != 0 {
				want = step.indices[reference.Intn(len(step.indices))]
			}
			got, err := algorithm.Pick(step.candidates)
			if got != want || (err != nil) != (want == -1) {
				t.Fatalf("Pick() = %d, %v; want index %d", got, err, want)
			}
		})
	}

	if got, want := algorithm.RNG.Int63(), reference.Int63(); got != want {
		t.Fatalf("next RNG value = %d, want %d; selection consumed a different number of draws", got, want)
	}
}

func TestRandomZeroValueInitializesOnlyForSuccessfulSelection(t *testing.T) {
	algorithm := &algorithms.Random{}
	for _, candidates := range [][]algorithms.Candidate{
		nil,
		{{MaxConnections: 1, MappedConnections: 1}},
	} {
		if got, err := algorithm.Pick(candidates); got != -1 || err == nil || algorithm.RNG != nil {
			t.Fatalf("unavailable Pick() = %d, %v; RNG initialized = %v", got, err, algorithm.RNG != nil)
		}
	}

	// The zero value uses seed 1, including a draw for the only available candidate.
	reference := rand.New(rand.NewSource(1))
	reference.Intn(1)
	if got, err := algorithm.Pick([]algorithms.Candidate{{}}); got != 0 || err != nil || algorithm.RNG == nil {
		t.Fatalf("singleton Pick() = %d, %v; RNG initialized = %v", got, err, algorithm.RNG != nil)
	}
	if got, want := algorithm.RNG.Int63(), reference.Int63(); got != want {
		t.Fatalf("fallback RNG next value = %d, want %d", got, want)
	}
}
