package main

// payloadProgress counts unique payload bytes throughout one run, including
// unfinished or closed transfers. Headers and repeated data do not add bytes.
type payloadProgress struct {
	ReceivedBytes     uint64 `json:"receivedBytes"`
	AcknowledgedBytes uint64 `json:"acknowledgedBytes"`
}
