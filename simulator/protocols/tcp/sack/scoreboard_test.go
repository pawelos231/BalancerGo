package sack

import (
	"reflect"
	"testing"

	"load-balancer/model"
)

// Reports overlap and arrive in a different order; the sender keeps their union.
// Omitting a block later does not mean that the receiver discarded those bytes.
func TestSACKScoreboardMergesAndRetainsReports(t *testing.T) {
	var board Scoreboard
	board.Update(1, 100, []model.SACKBlock{{Start: 20, End: 30}, {Start: 5, End: 10}})
	board.Update(1, 100, []model.SACKBlock{{Start: 10, End: 25}})
	board.Update(1, 100, nil)

	want := []model.SACKBlock{{Start: 5, End: 30}}
	if !reflect.DeepEqual(board.ranges, want) {
		t.Fatalf("merged reports = %+v, want %+v", board.ranges, want)
	}
	if board.Update(1, 100, want) {
		t.Fatal("repeated reports counted as new evidence")
	}

	// ACK=25 confirms the prefix but preserves the report for the remaining bytes.
	board.Update(25, 100, nil)
	want = []model.SACKBlock{{Start: 25, End: 30}}
	if !reflect.DeepEqual(board.ranges, want) {
		t.Fatalf("reports after ACK = %+v, want %+v", board.ranges, want)
	}
}

// Bytes not sent yet, malformed ranges and duplicate-data SACK cannot invent progress.
func TestSACKScoreboardIgnoresReportsOutsideSentData(t *testing.T) {
	var board Scoreboard
	changed := board.Update(10, 100, []model.SACKBlock{
		{Start: 1, End: 9},
		{Start: 10, End: 20},
		{Start: 20, End: 101},
		{Start: 30, End: 30},
	})
	if changed || len(board.ranges) != 0 {
		t.Fatalf("invalid reports changed scoreboard: %+v", board.ranges)
	}
}

// Two MSS above a gap are insufficient; one more byte or three separate blocks suffice.
func TestSACKScoreboardRequiresEvidenceOfLoss(t *testing.T) {
	var board Scoreboard
	board.Update(1, 10000, []model.SACKBlock{{Start: 1025, End: 3073}})
	if board.IsLost(1) {
		t.Fatal("two MSS alone declared a loss")
	}
	board.Update(1, 10000, []model.SACKBlock{{Start: 3073, End: 3074}})
	if !board.IsLost(1) || board.IsLost(2049) {
		t.Fatal("loss inference confused a gap with received bytes")
	}

	board.ranges = nil
	board.Update(1, 100, []model.SACKBlock{
		{Start: 5, End: 6}, {Start: 10, End: 11}, {Start: 15, End: 16},
	})
	if !board.IsLost(1) {
		t.Fatal("three separate reports did not identify the earlier gap")
	}
}

// [1,1025) looks lost, while [4097,5121) may still be travelling.
// Retrying the first gap adds its replacement copy to pipe.
func TestSACKPipeIncludesRetransmittedCopies(t *testing.T) {
	var board Scoreboard
	board.Update(1, 5121, []model.SACKBlock{{Start: 1025, End: 4097}})
	if got := board.Pipe(1, 5121, 1); got != 1024 {
		t.Fatalf("pipe before retry = %d, want 1024", got)
	}
	if got := board.Pipe(1, 5121, 1025); got != 2048 {
		t.Fatalf("pipe after retry = %d, want 2048", got)
	}

	// A speculative retry counts both the original and replacement as in flight.
	board.ranges = []model.SACKBlock{{Start: 1025, End: 2049}}
	if got := board.Pipe(1, 3073, 1025); got != 3072 {
		t.Fatalf("speculative pipe = %d, want 3072", got)
	}
}

// A block ending halfway through a segment leaves only its missing prefix to retry.
func TestSACKScoreboardSelectsPartialHolesAndTail(t *testing.T) {
	var board Scoreboard
	board.Update(1, 4097, []model.SACKBlock{{Start: 513, End: 2049}})
	first, found := board.NextHole(1, 4097, false)
	if !found || first != (model.SACKBlock{Start: 1, End: 513}) {
		t.Fatalf("first gap = %+v, found=%v", first, found)
	}
	last, found := board.LastHole(1, 4097)
	if !found || last != (model.SACKBlock{Start: 3073, End: 4097}) {
		t.Fatalf("rescue gap = %+v, found=%v", last, found)
	}
}
