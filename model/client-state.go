package model

type ClientState struct {
	Key       ClientKey
	RetrySyn  uint8
	RetryData uint8
}