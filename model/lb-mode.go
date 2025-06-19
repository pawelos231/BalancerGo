package model

type LBMode string

const (
	LBModeNAT  LBMode = "NAT"
	LBModeSNAT LBMode = "SNAT"
	LBModeDSR  LBMode = "DSR"
)
