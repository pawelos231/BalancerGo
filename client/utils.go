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

func GeneratePacket(key model.ClientKey, flag model.Flag, size int, delay time.Duration, placeInStream uint32) model.Packet {
	return model.Packet{
		Key:           key,
		Flag:          flag,
		Size:          size,
		Delay:         delay,
		PlaceInStream: placeInStream,
	}
}

func GenerateStream(clientKey model.ClientKey, dataSize int, delayBetween time.Duration) []model.Packet {
	flags := []model.Flag{model.FlagACK}

	var packets []model.Packet
	remaining := dataSize
	for remaining > 0 {
		chunk := model.SEGMENT_SIZE
		if chunk > remaining {
			chunk = remaining
		}
		// Insert PSH flag after ACK
		flags = append(flags, model.FlagPSH)
		remaining -= chunk
	}

	for i, flag := range flags {
		packet := GeneratePacket(clientKey, flag, model.SEGMENT_SIZE, delayBetween, uint32(i))
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
		packet := GeneratePacket(clientKey, flag, sizes[i], delay, uint32(i))
		packets = append(packets, packet)
	}
	return packets
}
