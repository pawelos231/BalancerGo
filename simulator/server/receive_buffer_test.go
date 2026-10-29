package server

import (
	"math"
	"testing"
)

func TestReceiveBufferMergesRangesAndCountsOnlyNewBytes(t *testing.T) {
	buffer := receiveBuffer{nextExpectedSeq: 1, capacity: 16}

	// Later data waits for gaps; overlapping copies contribute only their new bytes.
	steps := []struct {
		start, end    uint64
		newBytes      int
		nextExpected  uint64
		bufferedBytes int
	}{
		{start: 9, end: 13, newBytes: 4, nextExpected: 1, bufferedBytes: 4},
		{start: 5, end: 11, newBytes: 4, nextExpected: 1, bufferedBytes: 8},
		{start: 9, end: 13, newBytes: 0, nextExpected: 1, bufferedBytes: 8},
		{start: 15, end: 17, newBytes: 2, nextExpected: 1, bufferedBytes: 10},
		{start: 1, end: 7, newBytes: 4, nextExpected: 13, bufferedBytes: 2},
		{start: 13, end: 15, newBytes: 2, nextExpected: 17, bufferedBytes: 0},
		{start: 1, end: 17, newBytes: 0, nextExpected: 17, bufferedBytes: 0},
	}
	for _, step := range steps {
		newBytes := buffer.accept(step.start, step.end)
		if newBytes != step.newBytes || buffer.nextExpectedSeq != step.nextExpected ||
			buffer.bufferedBytes != step.bufferedBytes {
			t.Fatalf("receive [%d, %d): new=%d next=%d buffered=%d; want %+v",
				step.start, step.end, newBytes, buffer.nextExpectedSeq,
				buffer.bufferedBytes, step)
		}

		// Buffered ranges occupy positions inside the window; they do not shrink it.
		if got := buffer.windowBytes(); got != 16 {
			t.Fatalf("receive window = %d, want 16", got)
		}
	}
	if len(buffer.ranges) != 0 {
		t.Fatal("contiguous data still occupies receive ranges")
	}
}

func TestReceiveBufferClipsDataAndLeavesRoomForMissingBytes(t *testing.T) {
	buffer := receiveBuffer{nextExpectedSeq: 1, capacity: 8}

	// Bytes beyond position 8 are outside this window, even within one packet.
	if got := buffer.accept(4, 100); got != 5 || buffer.bufferedBytes != 5 {
		t.Fatalf("clipped data: new=%d buffered=%d, want 5 and 5", got, buffer.bufferedBytes)
	}
	if got := buffer.accept(9, 12); got != 0 {
		t.Fatalf("accepted %d bytes beyond the window", got)
	}

	// Receiving the missing prefix frees all eight positions at once.
	if got := buffer.accept(1, 4); got != 3 || buffer.nextExpectedSeq != 9 || buffer.bufferedBytes != 0 {
		t.Fatalf("missing prefix: new=%d next=%d buffered=%d", got, buffer.nextExpectedSeq, buffer.bufferedBytes)
	}
	if got := buffer.accept(7, 11); got != 2 || buffer.nextExpectedSeq != 11 {
		t.Fatalf("old prefix was counted twice: new=%d next=%d", got, buffer.nextExpectedSeq)
	}
}

func TestReceiveWindowDoesNotWrapAtLogicalSequenceLimit(t *testing.T) {
	buffer := receiveBuffer{nextExpectedSeq: math.MaxUint64 - 4, capacity: 8}
	if got := buffer.windowBytes(); got != 4 {
		t.Fatalf("window near sequence limit = %d, want 4", got)
	}

	buffer.accept(math.MaxUint64-2, math.MaxUint64)
	buffer.accept(math.MaxUint64-4, math.MaxUint64-2)
	if buffer.nextExpectedSeq != math.MaxUint64 || buffer.windowBytes() != 0 || buffer.bufferedBytes != 0 {
		t.Fatalf("sequence limit wrapped or retained data: %+v", buffer)
	}
}
