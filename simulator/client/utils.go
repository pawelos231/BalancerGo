package client

import (
	"load-balancer/model"
	"time"
)

func GenerateClientKey(srcIP string, srcPort int16, dstIP string, dstPort int16, protocol model.Protocol) model.ClientKey {
	return model.ClientKey{
		SrcIP:    srcIP,
		SrcPort:  srcPort,
		DstIP:    dstIP,
		DstPort:  dstPort,
		Protocol: protocol,
	}
}

func GeneratePacket(key model.ClientKey, flag model.Flag, size int, delay time.Duration, placeInStream uint32) *model.Packet {
	return &model.Packet{
		Key:           key,
		Flag:          flag,
		Size:          size,
		Delay:         delay,
		PlaceInStream: placeInStream,
	}
}
