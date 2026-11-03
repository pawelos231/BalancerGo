package client

import "load-balancer/model"

// sackScoreboard remembers bytes reported by SACK until a cumulative ACK covers them.
// For example, ACK=1 and SACK=[1025,4097) leave the first 1024 bytes missing.
type sackScoreboard struct {
	ranges []model.SACKBlock
}

// update merges new reports; an omitted block does not undo an earlier report.
// Blocks outside sent data are ignored, including unsupported duplicate-data SACK.
func (board *sackScoreboard) update(ack, sentEnd uint64, blocks []model.SACKBlock) bool {
	board.discardBefore(ack)
	before := board.receivedBytes()
	for _, block := range blocks {
		if block.Start <= ack || block.Start >= block.End || block.End > sentEnd {
			continue
		}
		board.insert(block)
	}
	return board.receivedBytes() > before
}

// discardBefore releases reports already covered by the ordinary ACK.
func (board *sackScoreboard) discardBefore(ack uint64) {
	remaining := 0
	for _, block := range board.ranges {
		if block.End <= ack {
			continue
		}
		block.Start = max(block.Start, ack)
		board.ranges[remaining] = block
		remaining++
	}
	clear(board.ranges[remaining:])
	board.ranges = board.ranges[:remaining]
}

// insert keeps reports sorted and joins overlaps, including adjacent blocks.
// [1025,2049) and [2049,3073) become one [1025,3073) report.
func (board *sackScoreboard) insert(incoming model.SACKBlock) {
	first := 0
	for first < len(board.ranges) && board.ranges[first].End < incoming.Start {
		first++
	}
	afterLast := first
	for afterLast < len(board.ranges) && board.ranges[afterLast].Start <= incoming.End {
		incoming.Start = min(incoming.Start, board.ranges[afterLast].Start)
		incoming.End = max(incoming.End, board.ranges[afterLast].End)
		afterLast++
	}

	if first == afterLast {
		board.ranges = append(board.ranges, model.SACKBlock{})
		copy(board.ranges[first+1:], board.ranges[first:])
	} else {
		remaining := copy(board.ranges[first+1:], board.ranges[afterLast:])
		newLength := first + 1 + remaining
		clear(board.ranges[newLength:])
		board.ranges = board.ranges[:newLength]
	}
	board.ranges[first] = incoming
}

// receivedBytes counts unique reported bytes, so overlapping reports count only once.
func (board *sackScoreboard) receivedBytes() uint64 {
	var total uint64
	for _, block := range board.ranges {
		total += block.End - block.Start
	}
	return total
}

// isLost needs three separate reports above a hole or more than two MSS of data.
// One later segment can simply mean reordering; it is not enough evidence of loss.
// RFC 6675 section 4: https://www.rfc-editor.org/rfc/rfc6675.html#section-4
func (board *sackScoreboard) isLost(sequence uint64) bool {
	blocksAbove := 0
	var bytesAbove uint64
	for _, block := range board.ranges {
		if block.Start <= sequence {
			if sequence < block.End {
				return false
			}
			continue
		}
		blocksAbove++
		bytesAbove += block.End - block.Start
	}
	return blocksAbove >= 3 || bytesAbove > 2*model.SEGMENT_SIZE
}

// holes visits missing ranges, without iterating over individual bytes.
// Reports [5,9), [13,17) inside [1,20) leave [1,5), [9,13), [17,20).
func (board *sackScoreboard) holes(start, end uint64, visit func(model.SACKBlock) bool) {
	for _, block := range board.ranges {
		if block.End <= start {
			continue
		}
		if block.Start >= end {
			break
		}
		if start < block.Start && !visit(model.SACKBlock{Start: start, End: block.Start}) {
			return
		}
		start = max(start, block.End)
	}
	if start < end {
		visit(model.SACKBlock{Start: start, End: end})
	}
}

// pipe counts unsacked bytes still thought to be travelling, plus retransmitted copies.
// A speculative retry can count twice: the original might still be on its way.
func (board *sackScoreboard) pipe(start, end, retransmittedEnd uint64) uint64 {
	var total uint64
	board.holes(start, end, func(hole model.SACKBlock) bool {
		if !board.isLost(hole.Start) {
			total += hole.End - hole.Start
		}
		if hole.Start < retransmittedEnd {
			total += min(hole.End, retransmittedEnd) - hole.Start
		}
		return true
	})
	return total
}

// nextHole picks the earliest eligible gap and limits its retry to one MSS.
func (board *sackScoreboard) nextHole(start, end uint64, requireLoss bool) (model.SACKBlock, bool) {
	var selected model.SACKBlock
	board.holes(start, end, func(hole model.SACKBlock) bool {
		if requireLoss && !board.isLost(hole.Start) {
			return true
		}
		selected = hole
		selected.End = selected.Start + min(selected.End-selected.Start, uint64(model.SEGMENT_SIZE))
		return false
	})
	return selected, selected.Start < selected.End
}

// lastHole supplies a single rescue retry containing the highest missing byte.
func (board *sackScoreboard) lastHole(start, end uint64) (model.SACKBlock, bool) {
	var selected model.SACKBlock
	board.holes(start, end, func(hole model.SACKBlock) bool {
		selected = hole
		selected.Start = selected.End - min(selected.End-selected.Start, uint64(model.SEGMENT_SIZE))
		return true
	})
	return selected, selected.Start < selected.End
}
