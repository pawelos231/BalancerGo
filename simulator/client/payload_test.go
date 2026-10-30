package client_test

import (
	"slices"
	"testing"
	"time"

	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/model"
)

// Three segments are sent before ACK; one cumulative ACK confirms the first two.
func TestAcknowledgedPayloadCountsUniqueProgress(t *testing.T) {
	h := newTransferHarness(t)
	var increments []int
	var observedTotals []uint64
	h.client.OnPayloadAcknowledged = func(bytes int) {
		increments = append(increments, bytes)
		observedTotals = append(observedTotals, h.client.GetState().AcknowledgedPayloadBytes)
	}
	var totalAtCompletion uint64
	h.client.OnStreamComplete = func() {
		totalAtCompletion = h.client.GetState().AcknowledgedPayloadBytes
	}

	assertTotal := func(want uint64) {
		t.Helper()
		if got := h.client.GetState().AcknowledgedPayloadBytes; got != want {
			t.Fatalf("acknowledged payload = %d, want %d", got, want)
		}
	}

	const finalBytes = 37
	const streamBytes = 2*model.SEGMENT_SIZE + finalBytes
	h.client.SendCompleteStream(streamBytes, time.Millisecond)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagACK, 1)
	assertTotal(0)

	// With enough window credit, 1024 + 1024 + 37 bytes leave before any data ACK.
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(3 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 3 || data[0].Size != 1024 || data[1].Size != 1024 || data[2].Size != finalBytes {
		t.Fatalf("initial payload transmissions = %+v", data)
	}
	assertTotal(0)

	// ACK beyond the entire sent stream, old ACK and another flow confirm nothing.
	h.receive(model.FlagACK, 0)
	h.receive(model.FlagACK, streamBytes+2)
	otherFlow := h.client.Key()
	otherFlow.SrcPort++
	h.client.HandlePacket(model.Packet{
		Key:                otherFlow,
		Flag:               model.FlagACK,
		Seq:                1,
		Ack:                1025,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
	})

	// Only the oldest range is retried; merely sending it still confirms no bytes.
	h.runUntil(time.Millisecond + model.SEGMENT_TIMEOUT)
	data = sentClientData(h.sent)
	if len(data) != 4 || data[3] != data[0] {
		t.Fatalf("expected retransmission of the oldest segment, got %+v", data)
	}
	assertTotal(0)

	// ACK=2049 confirms both full segments in one callback; repeats add nothing.
	h.receive(model.FlagACK, 2049)
	h.receive(model.FlagACK, 2049)
	h.receive(model.FlagACK, 1025)
	assertTotal(2 * model.SEGMENT_SIZE)
	if h.client.StreamCompleted() {
		t.Fatal("stream completed before its partial final segment was acknowledged")
	}

	h.receive(model.FlagACK, 1+streamBytes)
	assertTotal(streamBytes)
	h.receive(model.FlagACK, 1+streamBytes)
	if !h.client.StreamCompleted() || totalAtCompletion != streamBytes {
		t.Fatalf("completion saw %d acknowledged bytes; completed = %v",
			totalAtCompletion, h.client.StreamCompleted())
	}
	if !slices.Equal(increments, []int{2 * model.SEGMENT_SIZE, finalBytes}) {
		t.Fatalf("payload callbacks = %v", increments)
	}
	if !slices.Equal(observedTotals, []uint64{2 * model.SEGMENT_SIZE, streamBytes}) {
		t.Fatalf("payload totals visible in callbacks = %v", observedTotals)
	}

	// FIN and later shutdown cannot change already confirmed payload.
	h.runUntil(h.engine.Now())
	h.receive(model.FlagFIN|model.FlagACK, 2+streamBytes)
	h.client.Shutdown()
	assertTotal(streamBytes)
	fresh := client.NewClient(h.engine, "10.0.0.1", 1000, "10.0.0.2", 80, model.TCP)
	if got := fresh.GetState().AcknowledgedPayloadBytes; got != 0 {
		t.Fatalf("fresh client acknowledged payload = %d, want 0", got)
	}
}

// Receiving 1024 of 1047 bytes remains partial progress after closure or shutdown.
func TestAcknowledgedPayloadRetainsPartialTransferAfterClose(t *testing.T) {
	for _, finAck := range []bool{false, true} {
		name := "shutdown"
		if finAck {
			name = "FIN ACK with outstanding data"
		}
		t.Run(name, func(t *testing.T) {
			e := engine.New(time.Second)
			c := client.NewClient(e, "10.0.0.1", 1000, "10.0.0.2", 80, model.TCP)
			var data []model.Packet
			c.SetSender(func(pkt model.Packet) {
				if pkt.Flag&model.FlagPSH != 0 {
					data = append(data, pkt)
				}
			})
			c.SendCompleteStream(model.SEGMENT_SIZE+23, 0)
			c.EstablishConnection()
			if err := e.RunUntil(0); err != nil {
				t.Fatal(err)
			}
			c.HandlePacket(model.Packet{
				Key:                c.Key(),
				Flag:               model.FlagSYN | model.FlagACK,
				Ack:                1,
				ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
			})
			if err := e.RunUntil(0); err != nil {
				t.Fatal(err)
			}
			c.HandlePacket(model.Packet{
				Key:                c.Key(),
				Flag:               model.FlagACK,
				Seq:                1,
				Ack:                1025,
				ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
			})
			if err := e.RunUntil(0); err != nil {
				t.Fatal(err)
			}
			if len(data) != 2 || data[1].Size != 23 {
				t.Fatalf("expected unacknowledged final data, got %+v", data)
			}
			if finAck {
				// It acknowledges the outstanding range, but FIN still closes the transfer.
				c.HandlePacket(model.Packet{
					Key:                c.Key(),
					Flag:               model.FlagFIN | model.FlagACK,
					Seq:                1,
					Ack:                1048,
					ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
				})
			} else {
				c.Shutdown()
			}
			c.HandlePacket(model.Packet{
				Key:                c.Key(),
				Flag:               model.FlagACK,
				Seq:                1,
				Ack:                1048,
				ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
			})
			if got := c.GetState().AcknowledgedPayloadBytes; got != model.SEGMENT_SIZE {
				t.Fatalf("closed partial transfer acknowledged payload = %d, want %d", got, model.SEGMENT_SIZE)
			}
			if !c.IsTerminated() || c.StreamCompleted() {
				t.Fatalf("terminated = %v, completed = %v", c.IsTerminated(), c.StreamCompleted())
			}
		})
	}
}
