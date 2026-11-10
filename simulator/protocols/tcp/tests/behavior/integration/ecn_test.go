package tcp_test

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	tcpreceiver "load-balancer/protocols/tcp/receiver"
)

// ECN requires the asymmetric SYN offer (ECE+CWR) and SYN/ACK acceptance (ECE).
// A peer that declines it leaves every payload packet Not-ECT.
func TestECNNegotiationAndFallback(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, peerEnabled := range []bool{false, true} {
			e := engine.New(time.Second)
			sender := newTestSender(e)
			sender.SetECNEnabled(enabled)
			var packets, replies []model.Packet
			receiver := tcpreceiver.NewReceiver(e, tcpreceiver.ReceiverCallbacks{Send: func(pkt model.Packet) {
				replies = append(replies, pkt)
				sender.HandlePacket(pkt)
			}})
			receiver.SetECNEnabled(peerEnabled)
			sender.SetSender(func(pkt model.Packet) {
				packets = append(packets, pkt)
				receiver.HandlePacket(pkt)
			})
			sender.SetSource(newTestPayloadSource(e, 10, time.Millisecond))
			sender.EstablishConnection()
			if err := e.RunUntil(0); err != nil {
				t.Fatal(err)
			}
			wantOffer := model.FlagSYN
			if enabled {
				wantOffer |= model.FlagECE | model.FlagCWR
			}
			negotiated := enabled && peerEnabled
			if packets[0].Flag != wantOffer || sender.GetECNState().Negotiated != negotiated ||
				receiver.GetECNState().Negotiated != negotiated {
				t.Fatalf("enabled=%v peer=%v: SYN=%+v sender=%+v receiver=%+v", enabled, peerEnabled,
					packets[0], sender.GetECNState(), receiver.GetECNState())
			}
			// Repeated handshakes cannot renegotiate an existing connection.
			receiver.HandlePacket(model.Packet{Key: sender.Key(), Flag: model.FlagSYN})
			if replies[len(replies)-1].Flag != replies[0].Flag {
				t.Fatal("a repeated SYN changed the original ECN agreement")
			}
			sender.HandlePacket(model.Packet{Key: sender.Key(), Flag: model.FlagSYN | model.FlagACK, Ack: 1})
			if sender.GetECNState().Negotiated != negotiated {
				t.Fatal("a repeated SYN/ACK changed the original ECN agreement")
			}
			if err := e.RunUntil(time.Millisecond); err != nil {
				t.Fatal(err)
			}
			for _, pkt := range packets {
				wantECN := model.NotECT
				if pkt.Size > 0 && negotiated {
					wantECN = model.ECT0
				}
				if pkt.ECN.Codepoint != wantECN {
					t.Fatalf("unexpected codepoint on %+v", pkt)
				}
			}
			sender.Shutdown()
			receiver.Shutdown()
		}
	}
}
