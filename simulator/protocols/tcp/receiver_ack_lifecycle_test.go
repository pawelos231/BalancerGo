package tcp

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// Removing the endpoint or connection must not leave an ACK scheduled for 40 ms.
func TestDelayedACKCleanupCancelsThePendingReply(t *testing.T) {
	for _, removal := range []string{"shutdown", "forget", "reset", "idle expiry"} {
		t.Run(removal, func(t *testing.T) {
			test := newACKTestReceiver(t, func(receiver *Receiver) {
				if removal == "idle expiry" {
					receiver.SetIdleTimeouts(0, 20*time.Millisecond)
				}
			})
			test.data(1, 10)

			switch removal {
			case "shutdown":
				test.receiver.Shutdown()
			case "forget":
				test.receiver.ForgetConnection(test.key)
			case "reset":
				test.receiver.HandlePacket(model.Packet{
					Key: test.key, Flag: model.FlagRST | model.FlagACK, Seq: 11, Ack: 1,
				})
			}
			test.runUntil(t, 80*time.Millisecond)
			if len(test.replies) != 0 || test.engine.Pending() != 0 ||
				test.receiver.GetConnectionState(test.key).State != model.TCPClosed {
				t.Fatal("connection removal left a delayed reply or live timer")
			}
		})
	}
}

// FIN/ACK confirms data and the peer FIN together; a separate delayed ACK is redundant.
func TestFINACKCancelsThePendingDataACK(t *testing.T) {
	test := newACKTestReceiver(t, nil)
	var finACK model.Packet
	recordSend := test.receiver.callbacks.Send
	test.receiver.callbacks.Send = func(packet model.Packet) {
		recordSend(packet)
		if packet.Flag&model.FlagFIN != 0 {
			finACK = packet
		}
	}
	test.data(1, 10)
	test.receiver.HandlePacket(model.Packet{
		Key: test.key, Flag: model.FlagFIN | model.FlagACK, Seq: 11, Ack: 1,
	})
	test.runUntil(t, 80*time.Millisecond)
	if finACK.Ack != 12 || len(test.replies) != 0 {
		t.Fatal("FIN/ACK failed to confirm the data or left its old delayed ACK")
	}

	// The peer's final ACK releases FIN retry state as well.
	test.receiver.HandlePacket(model.Packet{Key: test.key, Flag: model.FlagACK, Seq: 12, Ack: 2})
	if test.engine.Pending() != 0 || test.receiver.GetACKState().SentACKCount != 0 {
		t.Fatal("graceful close retained a timer or counted FIN/ACK as a standalone ACK")
	}
}

// Sending an ACK can synchronously deliver new data. Its new timer belongs to
// that new segment and must survive the old callback returning.
func TestDelayedACKTimerResetsBeforeTheSendCallback(t *testing.T) {
	test := newACKTestReceiver(t, nil)
	recordSend := test.receiver.callbacks.Send
	firstACK := true
	test.receiver.callbacks.Send = func(packet model.Packet) {
		recordSend(packet)
		if firstACK {
			firstACK = false
			test.data(11, 10)
		}
	}
	test.data(1, 10)
	test.runUntil(t, 40*time.Millisecond)
	if len(test.replies) != 1 || test.engine.Pending() != 1 {
		t.Fatal("the first ACK callback lost the new segment's timer")
	}

	test.runUntil(t, 80*time.Millisecond)
	if len(test.replies) != 2 || test.replies[1].packet.Ack != 21 ||
		test.replies[1].at != 80*time.Millisecond || test.receiver.GetACKState().TimerTriggeredACKCount != 2 {
		t.Fatalf("newly received data did not keep its own deadline: %+v", test.replies)
	}
}

// Replacing a connection under the same key gives it its own pending ACK state.
func TestDelayedACKDoesNotTouchAReplacementConnection(t *testing.T) {
	test := newACKTestReceiver(t, nil)
	test.data(1, 10)
	test.receiver.ForgetConnection(test.key)
	test.receiver.HandlePacket(model.Packet{Key: test.key, Flag: model.FlagSYN, Seq: 100})
	test.receiver.HandlePacket(model.Packet{Key: test.key, Flag: model.FlagACK, Seq: 101, Ack: 1})
	test.runUntil(t, 10*time.Millisecond)
	test.data(101, 10)
	test.runUntil(t, 40*time.Millisecond)
	if len(test.replies) != 0 {
		t.Fatal("the removed connection acknowledged its replacement's data")
	}
	test.runUntil(t, 50*time.Millisecond)
	if len(test.replies) != 1 || test.replies[0].packet.Ack != 111 {
		t.Fatal("the replacement connection lost its own delayed ACK")
	}
}

// One initial MSS cannot release a second segment until the delayed ACK returns.
// This is the expected small-window penalty, without changing congestion-control growth.
func TestDelayedACKMakesAnInitialOneMSSSenderWait(t *testing.T) {
	e := engine.New(time.Second)
	sender := newTestSender(e)
	defer sender.Shutdown()
	congestion := model.DefaultCongestionConfig()
	congestion.InitialWindowSegments = 1
	sender.SetCongestionConfig(congestion)
	var payloadTimes []time.Duration
	receiver := NewReceiver(e, ReceiverCallbacks{Send: sender.HandlePacket})
	defer receiver.Shutdown()
	config := model.DefaultDelayedACKConfig()
	config.Enabled = true
	receiver.SetDelayedACKConfig(config)
	sender.SetSender(func(packet model.Packet) {
		if packet.Size > 0 {
			payloadTimes = append(payloadTimes, e.Now())
		}
		receiver.HandlePacket(packet)
	})
	sender.SetSource(newTestPayloadSource(e, 2*model.SEGMENT_SIZE, 0))
	sender.EstablishConnection()
	if err := e.RunUntil(40*time.Millisecond - time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	if len(payloadTimes) != 1 {
		t.Fatal("a one-MSS initial window sent the next segment without its ACK")
	}

	if err := e.RunUntil(80 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(payloadTimes) != 2 || payloadTimes[1] != 40*time.Millisecond || !sender.StreamCompleted() {
		t.Fatalf("the timer failed to release and complete the two segments: sends=%v", payloadTimes)
	}
}
