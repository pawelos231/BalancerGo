package model

type Flag uint8

const (
	FlagSYN Flag = 1 << iota // 1
	FlagFIN                  // 2
	FlagRST                  // 4
	FlagACK                  // 8
	FlagPSH                  // 16
	FlagURG                  // 32
	FlagECE                  // 64: TCP echoes congestion observed in the IP ECN field.
	FlagCWR                  // 128: the sender has reduced its congestion window.
)
