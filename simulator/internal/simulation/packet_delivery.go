package simulation

import (
	"load-balancer/model"
	"load-balancer/network"
)

// packetTransmitter lets each queue carry network metadata without knowing TCP.
type packetTransmitter interface {
	Transmit(int, func()) bool
	TransmitECN(int, network.ECNState, func(bool)) bool
}

// transmitPacket freezes one hop's packet and carries its CE mark to the next hop.
// The original sender's retained packet is never changed by a network queue.
func transmitPacket(link packetTransmitter, packet model.Packet, arrive func(model.Packet)) bool {
	if packet.ECN.Codepoint == model.NotECT {
		return link.Transmit(packet.WireSize(), func() { arrive(packet) })
	}
	state := network.Capable
	if packet.ECN.Codepoint == model.CE {
		state = network.AlreadyMarked
	}
	return link.TransmitECN(packet.WireSize(), state, func(marked bool) {
		if marked {
			packet.ECN.Codepoint = model.CE
		}
		arrive(packet)
	})
}
