package tcp

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// A lost ECE ACK must not lose the signal. Valid CWR clears it, and CE on that
// same packet starts fresh feedback. Invalid receive positions cannot clear ECE.
func TestReceiverECNEchoPersistsThroughCWRAndFIN(t *testing.T) {
	e := engine.New(time.Second)
	var replies []model.Packet
	receiver := NewReceiver(e, ReceiverCallbacks{Send: func(pkt model.Packet) { replies = append(replies, pkt) }})
	defer receiver.Shutdown()
	receiver.SetECNEnabled(true)
	receiver.SetReceiveBufferBytes(16)
	key := newTestSender(e).Key()
	receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagSYN | model.FlagECE | model.FlagCWR})
	receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagACK, Seq: 1, Ack: 1})
	data := model.Packet{Key: key, Flag: model.FlagPSH | model.FlagACK, Seq: 1, Ack: 1, Size: 4,
		ECN: model.ECN{Codepoint: model.CE}}
	receiver.HandlePacket(data)
	if replies[len(replies)-1].Flag&model.FlagECE == 0 {
		t.Fatal("marked payload was not echoed")
	}
	// This CWR is outside the receive window. Its duplicate ACK keeps ECE.
	data.Seq, data.Flag, data.ECN.Codepoint = 100, model.FlagPSH|model.FlagACK|model.FlagCWR, model.NotECT
	receiver.HandlePacket(data)
	if replies[len(replies)-1].Flag&model.FlagECE == 0 {
		t.Fatal("out-of-window CWR cleared congestion feedback")
	}
	data.Seq, data.ECN.Codepoint = 5, model.CE
	receiver.HandlePacket(data)
	if replies[len(replies)-1].Flag&model.FlagECE == 0 {
		t.Fatal("CWR+CE lost the new congestion signal")
	}
	data.Seq, data.ECN.Codepoint = 9, model.ECT0
	receiver.HandlePacket(data)
	if replies[len(replies)-1].Flag&model.FlagECE != 0 {
		t.Fatal("valid CWR failed to clear the old congestion signal")
	}
	data.Seq, data.Flag, data.ECN.Codepoint = 13, model.FlagPSH|model.FlagACK, model.CE
	receiver.HandlePacket(data)
	receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagFIN | model.FlagACK, Seq: 17, Ack: 1})
	finACK := replies[len(replies)-1]
	if finACK.Flag != model.FlagFIN|model.FlagACK|model.FlagECE || finACK.ECN.Codepoint != model.NotECT {
		t.Fatalf("FIN/ACK omitted pending ECE or marked control traffic: %+v", finACK)
	}
	if state := receiver.GetECNState(); state.ReceivedCEPacketCount != 3 || state.SentECECount != 5 {
		t.Fatalf("receiver feedback counters are wrong: %+v", state)
	}
}

// Payload callbacks can remove the connection and replace it under the same key.
// ECN feedback from the old packet must never mutate or acknowledge the replacement.
func TestReceiverECNRespectsConnectionIdentityAfterPayloadCallback(t *testing.T) {
	e := engine.New(time.Second)
	key := newTestSender(e).Key()
	var receiver *Receiver
	var replies []model.Packet
	receiver = NewReceiver(e, ReceiverCallbacks{
		Send: func(pkt model.Packet) { replies = append(replies, pkt) },
		OnPayloadReceived: func(int) {
			receiver.ForgetConnection(key)
			receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagSYN, Seq: 100})
		},
	})
	defer receiver.Shutdown()
	receiver.SetECNEnabled(true)
	receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagSYN | model.FlagECE | model.FlagCWR})
	receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagACK, Seq: 1, Ack: 1, Size: 4,
		ECN: model.ECN{Codepoint: model.CE}})
	if len(replies) != 2 || replies[1].Flag != model.FlagSYN|model.FlagACK ||
		receiver.connections[key].ecnEcho || receiver.GetECNState().Negotiated {
		t.Fatalf("old ECN receive affected the replacement connection: replies=%+v state=%+v", replies, receiver.GetECNState())
	}
}

// Low-level callers get the same unsupported-profile guard as configuration users.
func TestECNRejectsNoCongestionControl(t *testing.T) {
	for _, probe := range []bool{false, true} {
		t.Run(map[bool]string{false: "connection", true: "probe"}[probe], func(t *testing.T) {
			sender := newTestSender(engine.New(time.Second))
			sender.SetECNEnabled(true)
			config := model.DefaultCongestionConfig()
			config.Mode = model.CongestionNone
			config.FastRetransmit = false
			sender.SetCongestionConfig(config)
			defer func() {
				if recover() == nil {
					t.Fatal("ECN with congestion control disabled did not fail before the SYN")
				}
			}()
			if probe {
				sender.SendSYNProbe()
			} else {
				sender.EstablishConnection()
			}
		})
	}
}
