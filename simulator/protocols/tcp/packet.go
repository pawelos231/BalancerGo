package tcp

import "load-balancer/model"

// GeneratePacket constructs a logical TCP segment; the sender supplies its receive window.
func GeneratePacket(key model.ClientKey, flag model.Flag, size int, seq, ack uint64) model.Packet {
	return model.Packet{Key: key, Flag: flag, Size: size, Seq: seq, Ack: ack}
}
