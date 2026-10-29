package server

import "math"

// byteRange holds received positions from start up to, but excluding, end.
// so for example something like this: [5, 17)
type byteRange struct {
	start uint64
	end   uint64
}

type receiveBuffer struct {
	nextExpectedSeq uint64
	capacity        uint16
	bufferedBytes   int
	ranges          []byteRange
}

// windowBytes limits the accepted sequence range without wrapping its right edge.
func (buffer *receiveBuffer) windowBytes() uint16 {
	remainingPositions := math.MaxUint64 - buffer.nextExpectedSeq
	cap := uint64(buffer.capacity)
	return uint16(min(cap, remainingPositions))
}

// overlapsWindow checks whether any payload byte can be accepted.
func (buffer *receiveBuffer) overlapsWindow(start, end uint64) bool {
	windowEnd := buffer.nextExpectedSeq + uint64(buffer.windowBytes())
	return start < windowEnd && end > buffer.nextExpectedSeq
}

// accept merges incoming data with ranges buffered for this backend connection.
// It skips duplicate bytes, respects the window and advances ACK when a gap is filled.
func (buffer *receiveBuffer) accept(start, end uint64) int {
	windowEnd := buffer.nextExpectedSeq + uint64(buffer.windowBytes())
	start = max(start, buffer.nextExpectedSeq)
	end = min(end, windowEnd)
	if start >= end {
		return 0
	}

	// Find the existing ranges touched by this data, including adjacent ranges.
	firstRangeIndex := 0
	for firstRangeIndex < len(buffer.ranges) && buffer.ranges[firstRangeIndex].end < start {
		firstRangeIndex++
	}

	merged := byteRange{start: start, end: end}
	newBytes := end - start
	afterLastRangeIndex := firstRangeIndex

	// merge buffered ranges that overlap or touch the incoming data
	for afterLastRangeIndex < len(buffer.ranges) && buffer.ranges[afterLastRangeIndex].start <= merged.end {
		existing := buffer.ranges[afterLastRangeIndex]
		overlapStart := max(start, existing.start)
		overlapEnd := min(end, existing.end)
		if overlapStart < overlapEnd {
			// Exclude bytes already received so they are not counted again.
			duplicateBytes := overlapEnd - overlapStart
			newBytes -= duplicateBytes
		}

		merged.start = min(merged.start, existing.start)
		merged.end = max(merged.end, existing.end)
		afterLastRangeIndex++
	}
	buffer.bufferedBytes += int(newBytes)

	// Filling the earliest gap also releases any following ranges already received.
	if merged.start == buffer.nextExpectedSeq {
		consumedBytes := merged.end - buffer.nextExpectedSeq
		buffer.nextExpectedSeq = merged.end
		buffer.bufferedBytes -= int(consumedBytes)
		buffer.replaceRanges(firstRangeIndex, afterLastRangeIndex, nil)
		return int(newBytes)
	}

	buffer.replaceRanges(firstRangeIndex, afterLastRangeIndex, &merged)
	return int(newBytes)
}

// replaceRanges replaces the selected ranges with merged; nil only removes them.
// The end index is excluded: replacing indexes 0:2 in [5,9), [13,17), [30,40)
// with [5,17) gives [5,17), [30,40).
func (buffer *receiveBuffer) replaceRanges(firstRangeIndex, afterLastRangeIndex int, merged *byteRange) {
	// No existing ranges to replace: make room and insert the new range.
	// Example: [5,9), [20,25) + [12,15) at index 1 -> [5,9), [12,15), [20,25).
	if firstRangeIndex == afterLastRangeIndex && merged != nil {
		buffer.ranges = append(buffer.ranges, byteRange{})
		copy(buffer.ranges[firstRangeIndex+1:], buffer.ranges[firstRangeIndex:])
		buffer.ranges[firstRangeIndex] = *merged
		return
	}

	writeAt := firstRangeIndex
	if merged != nil {
		buffer.ranges[writeAt] = *merged
		writeAt++
	}

	remaining := copy(buffer.ranges[writeAt:], buffer.ranges[afterLastRangeIndex:])
	newLength := writeAt + remaining
	clear(buffer.ranges[newLength:])
	buffer.ranges = buffer.ranges[:newLength]
}
