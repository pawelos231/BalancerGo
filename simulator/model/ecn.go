package model

// ECNCodepoint models the two IP ECN bits, not a TCP option or TCP flag.
// RFC 3168 section 5: https://www.rfc-editor.org/rfc/rfc3168.html#section-5
type ECNCodepoint uint8

const (
	NotECT ECNCodepoint = iota // This packet cannot be marked instead of dropped.
	ECT1                       // Classic ECN accepts either ECT codepoint.
	ECT0                       // This simulator sends new negotiated payload as ECT(0).
	CE                         // A queue reports congestion without discarding the packet.
)

// ECN carries the IP congestion signal (RFC 3168 section 5), independently of TCP flags.
// https://www.rfc-editor.org/rfc/rfc3168.html#section-5
type ECN struct {
	Codepoint ECNCodepoint
}

// ECNState counts transport feedback, independently of network queue marks.
// Receiver Negotiated reports whether any current connection negotiated ECN.
type ECNState struct {
	Enabled               bool
	Negotiated            bool
	ReceivedCEPacketCount uint64
	SentECECount          uint64
	ReceivedECECount      uint64
	SentCWRCount          uint64
	ECNResponseCount      uint64
}
