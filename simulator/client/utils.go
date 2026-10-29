package client

import "load-balancer/model"

func GenerateClientKey(
	srcIP string,
	srcPort uint16,
	dstIP string,
	dstPort uint16,
	protocol model.Protocol,
) model.ClientKey {
	return model.ClientKey{
		SrcIP:    srcIP,
		SrcPort:  srcPort,
		DstIP:    dstIP,
		DstPort:  dstPort,
		Protocol: protocol,
	}
}

func GeneratePacket(
	key model.ClientKey,
	flag model.Flag,
	size int,
	seq, ack uint64,
) model.Packet {
	return model.Packet{
		Key:  key,
		Flag: flag,
		Size: size,
		Seq:  seq,
		Ack:  ack,
	}
}
