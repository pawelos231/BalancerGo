package sack

import (
	"testing"

	"load-balancer/model"
)

// With MSS=1460, exactly 2920 SACKed bytes do not prove loss; one extra byte does.
func TestSelectedMSSDefinesLossEvidence(t *testing.T) {
	var board Scoreboard
	board.Update(1, 6000, []model.SACKBlock{{Start: 1461, End: 4381}})
	if board.IsLost(1, 1460) {
		t.Fatal("exactly two MSS should not prove loss")
	}
	if got := board.Pipe(1, 4381, 1, 1460); got != 1460 {
		t.Fatalf("unproven first segment should remain in Pipe: got %d", got)
	}

	board.Update(1, 6000, []model.SACKBlock{{Start: 4381, End: 4382}})
	if !board.IsLost(1, 1460) {
		t.Fatal("more than two MSS should prove loss")
	}
	if got := board.Pipe(1, 4382, 1, 1460); got != 0 {
		t.Fatalf("lost first segment should leave Pipe: got %d", got)
	}
}

// A large hole is retried one configured MSS at a time, from either end.
func TestSelectedMSSLimitsHoleRetries(t *testing.T) {
	var board Scoreboard
	board.Update(1, 10000, []model.SACKBlock{{Start: 6001, End: 7001}})

	first, found := board.NextHole(1, 10000, false, 1460)
	if !found || first.Start != 1 || first.End != 1461 {
		t.Fatalf("first retry should cover [1,1461): %+v, found=%v", first, found)
	}

	last, found := board.LastHole(1, 10000, 1460)
	if !found || last.Start != 8540 || last.End != 10000 {
		t.Fatalf("rescue retry should cover [8540,10000): %+v, found=%v", last, found)
	}
}
