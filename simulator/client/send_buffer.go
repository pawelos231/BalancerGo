package client

import "load-balancer/model"

// sendBuffer keeps sent segments in sequence order until their ACK arrives.
// For example, [1,1025) stays here while the backend still reports ACK=1.
type sendBuffer struct {
	segments          []model.Packet
	firstPendingIndex int
}

// hasPending reports whether any sent payload still needs an ACK.
func (buffer *sendBuffer) hasPending() bool {
	return buffer.firstPendingIndex < len(buffer.segments)
}

// oldest is the first fragment to retry; callers first check hasPending.
func (buffer *sendBuffer) oldest() model.Packet {
	return buffer.segments[buffer.firstPendingIndex]
}

// append records a newly sent segment after all earlier unconfirmed segments.
func (buffer *sendBuffer) append(packet model.Packet) {
	buffer.segments = append(buffer.segments, packet)
}

// acknowledge removes confirmed segments and trims the first remaining fragment.
// Example: [1,1025), [1025,2049) with ACK=1500 leaves only [1500,2049).
func (buffer *sendBuffer) acknowledge(ack uint64) {
	for buffer.hasPending() {
		packet := &buffer.segments[buffer.firstPendingIndex]
		// Stop if ACK has not advanced past the start of the first pending segment.
		if ack <= packet.Seq {
			break
		}

		segmentEnd := packet.Seq + uint64(packet.Size)
		if ack < segmentEnd {
			// The ACK ends inside this segment; retain only its missing suffix.
			packet.Size = int(segmentEnd - ack)
			packet.Seq = ack
			break
		}

		buffer.segments[buffer.firstPendingIndex] = model.Packet{}
		buffer.firstPendingIndex++
	}

	remainingSegments := len(buffer.segments) - buffer.firstPendingIndex
	if remainingSegments == 0 {
		buffer.segments = buffer.segments[:0]
		buffer.firstPendingIndex = 0
		return
	}

	// Compact when there are at least as many acknowledged slots as pending segments.
	// Example: ACK confirms 3 of 5 segments: firstPendingIndex=3, remainingSegments=2.
	// [empty, empty, empty, A, B] becomes [A, B], with firstPendingIndex reset to 0.
	if buffer.firstPendingIndex >= remainingSegments {
		copy(buffer.segments, buffer.segments[buffer.firstPendingIndex:])
		clear(buffer.segments[remainingSegments:])
		buffer.segments = buffer.segments[:remainingSegments]
		buffer.firstPendingIndex = 0
	}
}

// reset releases packet metadata when the connection closes or is interrupted.
func (buffer *sendBuffer) reset() {
	buffer.segments = nil
	buffer.firstPendingIndex = 0
}
