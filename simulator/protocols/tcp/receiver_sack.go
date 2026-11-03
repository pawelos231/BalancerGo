package tcp

import "load-balancer/model"

// SetSACKEnabled allows range reports when the client's first SYN offers SACK.
func (r *Receiver) SetSACKEnabled(enabled bool) {
	if r.trafficStarted || len(r.connections) != 0 {
		panic("tcp: SACK must be configured before traffic")
	}
	r.sackEnabled = enabled
}

// recordSACKArrival puts the block touched by this segment first, then repeats recent blocks.
// Receiving [9,13) after [5,9) reports their merged block [5,13).
func (conn *receiveConnection) recordSACKArrival(start, end uint64) {
	if !conn.sackPermitted {
		return
	}

	var recent [model.MaxSACKBlocks]model.SACKBlock
	count := 0
	include := func(received byteRange) {
		block := model.SACKBlock{Start: received.start, End: received.end}
		for _, existing := range recent[:count] {
			if existing == block {
				return
			}
		}
		if count < len(recent) {
			recent[count] = block
			count++
		}
	}

	// RFC 2018 section 4: the triggering segment's buffered block comes first.
	for _, received := range conn.receiver.ranges {
		if received.start < end && received.end > start {
			include(received)
			break
		}
	}

	// Old reports may now belong to a larger merged range, or have been consumed.
	for _, previous := range conn.sackRecent[:conn.sackRecentCount] {
		if count == len(recent) {
			break
		}
		for _, received := range conn.receiver.ranges {
			if received.start < previous.End && received.end > previous.Start {
				include(received)
				break
			}
		}
	}
	for _, received := range conn.receiver.ranges {
		if count == len(recent) {
			break
		}
		include(received)
	}
	conn.sackRecent = recent
	conn.sackRecentCount = count
}

// sackBlocks copies the current report so an in-flight ACK cannot change later.
func (conn *receiveConnection) sackBlocks() []model.SACKBlock {
	if conn.sackRecentCount == 0 {
		return nil
	}
	return append([]model.SACKBlock(nil), conn.sackRecent[:conn.sackRecentCount]...)
}
