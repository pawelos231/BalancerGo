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

func GeneratePacket(key model.ClientKey, flag model.Flag, size int, delay time.Duration) model.Packet {
	return model.Packet{
		Key:   key,
		Flag:  flag,
		Size:  size,
		Delay: delay,
	}
}

func GenerateStream(clientKey model.ClientKey, dataSize int, delayBetween time.Duration) []model.Packet {
	var packets []model.Packet
	flags := []model.Flag{model.FlagSYN, model.FlagACK, model.FlagPSH, model.FlagFIN}
	sizes := []int{0, 0, dataSize, 0}

	for i, flag := range flags {
		packet := GeneratePacket(clientKey, flag, sizes[i], delayBetween*time.Duration(i))
		packets = append(packets, packet)
	}
	return packets
}

func GenerateStreamWithDelays(clientKey model.ClientKey, dataSize int, delays []time.Duration) []model.Packet {
	var packets []model.Packet
	flags := []model.Flag{model.FlagSYN, model.FlagACK, model.FlagPSH, model.FlagFIN}
	sizes := []int{0, 0, dataSize, 0}

	for i, flag := range flags {
		delay := 10 * time.Millisecond
		if i < len(delays) {
			delay = delays[i]
		}
		packet := GeneratePacket(clientKey, flag, sizes[i], delay)
		packets = append(packets, packet)
	}
	return packets
}
