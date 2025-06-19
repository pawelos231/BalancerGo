package model

type Flag uint8

const (
	FlagSYN Flag = 1 << iota // 1
	FlagFIN                  // 2
	FlagRST                  // 4
	FlagACK                  // 8
	FlagPSH                  // 16
	FlagURG                  // 32
)
