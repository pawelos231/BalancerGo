package tcp

import (
	"strconv"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

type timedACK struct {
	at     time.Duration
	packet model.Packet
}

type ackTestReceiver struct {
	engine   *engine.Engine
	receiver *Receiver
	key      model.ClientKey
	replies  []timedACK
}

// The peer offers ECN/SACK; each test can enable those receiver capabilities before SYN.
// Only standalone ACKs are collected, so handshake and FIN replies do not hide assertions.
func newACKTestReceiver(t *testing.T, configure func(*Receiver)) *ackTestReceiver {
	t.Helper()
	test := &ackTestReceiver{
		engine: engine.New(time.Second),
		key:    model.ClientKey{SrcIP: "10.0.0.1", DstIP: "10.0.0.2", Protocol: model.TCP},
	}
	test.receiver = NewReceiver(test.engine, ReceiverCallbacks{Send: func(packet model.Packet) {
		if packet.Flag&(model.FlagSYN|model.FlagFIN|model.FlagRST) == 0 {
			test.replies = append(test.replies, timedACK{at: test.engine.Now(), packet: packet})
		}
	}})
	config := model.DefaultDelayedACKConfig()
	config.Enabled = true
	test.receiver.SetDelayedACKConfig(config)
	test.receiver.SetIdleTimeouts(0, 0)
	if configure != nil {
		configure(test.receiver)
	}
	t.Cleanup(test.receiver.Shutdown)
	test.receiver.HandlePacket(model.Packet{
		Key: test.key, Flag: model.FlagSYN | model.FlagECE | model.FlagCWR,
		SACKPermitted: true,
	})
	test.receiver.HandlePacket(model.Packet{Key: test.key, Flag: model.FlagACK, Seq: 1, Ack: 1})
	return test
}

func (test *ackTestReceiver) data(sequence uint64, size int) {
	test.receiver.HandlePacket(model.Packet{
		Key: test.key, Flag: model.FlagACK | model.FlagPSH,
		Seq: sequence, Ack: 1, Size: size,
	})
}

func (test *ackTestReceiver) runUntil(t *testing.T, at time.Duration) {
	t.Helper()
	if err := test.engine.RunUntil(at); err != nil {
		t.Fatal(err)
	}
}

// Two consecutive segments, even when short, share one cumulative ACK.
func TestDelayedACKConfirmsTwoSegmentsTogether(t *testing.T) {
	test := newACKTestReceiver(t, nil)
	test.data(1, 1000)
	if len(test.replies) != 0 {
		t.Fatal("the first segment was acknowledged before its partner arrived")
	}

	test.runUntil(t, 10*time.Millisecond)
	test.data(1001, 24)
	if len(test.replies) != 1 || test.replies[0].packet.Ack != 1025 || test.replies[0].at != 10*time.Millisecond {
		t.Fatalf("the second segment did not confirm both ranges: %+v", test.replies)
	}

	// Cancelling the first segment's deadline prevents another ACK at 40 ms.
	test.runUntil(t, 80*time.Millisecond)
	state := test.receiver.GetACKState()
	if len(test.replies) != 1 || state.SentACKCount != 1 || state.TimerTriggeredACKCount != 0 {
		t.Fatalf("pair confirmation left a second ACK behind: replies=%+v state=%+v", test.replies, state)
	}
}

// A solitary final segment gets its ACK at the deadline, regardless of payload size.
// The receiver does not need to know that no more application data will follow.
func TestDelayedACKTimerConfirmsSingleSegment(t *testing.T) {
	for _, size := range []int{17, model.SEGMENT_SIZE} {
		t.Run(strconv.Itoa(size)+" bytes", func(t *testing.T) {
			test := newACKTestReceiver(t, nil)
			test.data(1, size)
			test.runUntil(t, 40*time.Millisecond-time.Nanosecond)
			if len(test.replies) != 0 {
				t.Fatal("the ACK timer fired early")
			}

			test.runUntil(t, 40*time.Millisecond)
			state := test.receiver.GetACKState()
			if len(test.replies) != 1 || test.replies[0].packet.Ack != 1+uint64(size) ||
				test.replies[0].at != 40*time.Millisecond || state.TimerTriggeredACKCount != 1 {
				t.Fatalf("the timer did not confirm the solitary segment: replies=%+v state=%+v", test.replies, state)
			}
		})
	}
}

// Disabled delayed ACK and a threshold of one both confirm each segment immediately.
func TestImmediateACKProfilesDoNotScheduleTimers(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "one segment"}[enabled], func(t *testing.T) {
			test := newACKTestReceiver(t, func(receiver *Receiver) {
				config := model.DefaultDelayedACKConfig()
				config.Enabled = enabled
				if enabled {
					config.SegmentsPerACK = 1
				}
				receiver.SetDelayedACKConfig(config)
			})
			test.data(1, 10)
			if len(test.replies) != 1 || test.replies[0].at != 0 || test.engine.Pending() != 0 {
				t.Fatal("an immediate-ACK profile deferred its reply")
			}
		})
	}
}

// A gap needs immediate feedback for fast retransmit; repairing it is urgent too.
func TestDelayedACKDoesNotHoldGapOrDuplicateFeedback(t *testing.T) {
	test := newACKTestReceiver(t, func(receiver *Receiver) { receiver.SetSACKEnabled(true) })
	test.data(1, 10)
	test.data(21, 10)
	if len(test.replies) != 1 || test.replies[0].packet.Ack != 11 ||
		len(test.replies[0].packet.SACKBlocks) != 1 || test.replies[0].packet.SACKBlocks[0].Start != 21 {
		t.Fatalf("out-of-order arrival did not report the gap and SACK range: %+v", test.replies)
	}

	test.data(11, 10)
	if len(test.replies) != 2 || test.replies[1].packet.Ack != 31 || len(test.replies[1].packet.SACKBlocks) != 0 {
		t.Fatalf("filling the gap was not acknowledged immediately: %+v", test.replies)
	}

	// A duplicate and an out-of-window arrival repeat ACK=31 without a timer.
	test.data(1, 10)
	test.data(100_000, 10)
	test.runUntil(t, 80*time.Millisecond)
	if len(test.replies) != 4 || test.engine.Pending() != 0 {
		t.Fatalf("urgent feedback left delayed work: %+v", test.replies)
	}
}

// New CE flushes pending feedback. CWR+CE starts a new indication even if ECE was set.
func TestDelayedACKKeepsFreshECNFeedbackImmediate(t *testing.T) {
	test := newACKTestReceiver(t, func(receiver *Receiver) { receiver.SetECNEnabled(true) })
	test.data(1, 10)
	marked := model.Packet{
		Key: test.key, Flag: model.FlagACK | model.FlagPSH,
		Seq: 11, Ack: 1, Size: 10, ECN: model.ECN{Codepoint: model.CE},
	}
	test.receiver.HandlePacket(marked)
	if len(test.replies) != 1 || test.replies[0].packet.Flag&model.FlagECE == 0 {
		t.Fatal("new CE did not flush an ECE ACK")
	}

	marked.Seq = 21
	marked.Flag |= model.FlagCWR
	test.receiver.HandlePacket(marked)
	if len(test.replies) != 2 || test.replies[1].packet.Ack != 31 || test.replies[1].packet.Flag&model.FlagECE == 0 {
		t.Fatal("CWR+CE failed to report new congestion immediately")
	}

	// Repeated CE in the same indication may wait, but its eventual ACK keeps ECE.
	marked.Seq = 31
	marked.Flag &^= model.FlagCWR
	test.receiver.HandlePacket(marked)
	test.runUntil(t, 40*time.Millisecond)
	if len(test.replies) != 3 || test.replies[2].packet.Flag&model.FlagECE == 0 ||
		test.receiver.GetACKState().TimerTriggeredACKCount != 1 {
		t.Fatal("the delayed ACK lost continuing ECN feedback")
	}
}

// A timer beyond EndTime must not be clamped into an ACK at the horizon.
func TestDelayedACKDoesNotSendEarlyAtRunEnd(t *testing.T) {
	test := newACKTestReceiver(t, nil)
	test.runUntil(t, test.engine.EndTime()-10*time.Millisecond)
	test.data(1, 10)
	test.runUntil(t, test.engine.EndTime())
	if len(test.replies) != 0 || test.receiver.GetACKState().TimerTriggeredACKCount != 0 {
		t.Fatal("a delayed ACK scheduled beyond the horizon was sent early")
	}
}
