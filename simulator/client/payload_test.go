package client_test

import (
	"slices"
	"testing"
	"time"

	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/model"
)

func TestAcknowledgedPayloadCountsUniqueProgress(t *testing.T) {
	e := engine.New(30 * time.Second)
	c := client.NewClient(e, "10.0.0.1", 1000, "10.0.0.2", 80, model.TCP)
	var sent []model.Packet
	c.SetSender(func(pkt model.Packet) { sent = append(sent, pkt) })
	var increments []int
	var observedTotals []uint64
	c.OnPayloadAcknowledged = func(bytes int) {
		increments = append(increments, bytes)
		observedTotals = append(observedTotals, c.GetState().AcknowledgedPayloadBytes)
	}
	var totalAtCompletion uint64
	c.OnStreamComplete = func() { totalAtCompletion = c.GetState().AcknowledgedPayloadBytes }
	assertTotal := func(want uint64) {
		t.Helper()
		if got := c.GetState().AcknowledgedPayloadBytes; got != want {
			t.Fatalf("acknowledged payload = %d, want %d", got, want)
		}
	}
	runUntil := func(until time.Duration) {
		t.Helper()
		if err := e.RunUntil(until); err != nil {
			t.Fatal(err)
		}
	}
	ack := func(nextByte uint64) {
		c.HandlePacket(model.Packet{
			Key: c.Key(), Flag: model.FlagACK, Seq: 1, Ack: nextByte,
		})
	}
	dataPackets := func() []model.Packet {
		var data []model.Packet
		for _, pkt := range sent {
			if pkt.Flag&model.FlagPSH != 0 {
				data = append(data, pkt)
			}
		}
		return data
	}

	const finalBytes = 37
	const streamBytes = 2*model.SEGMENT_SIZE + finalBytes
	c.SendCompleteStream(streamBytes, time.Millisecond)
	c.EstablishConnection()
	runUntil(0)
	ack(1)
	assertTotal(0)
	c.HandlePacket(model.Packet{Key: c.Key(), Flag: model.FlagSYN | model.FlagACK, Ack: 1})
	runUntil(time.Millisecond)
	data := dataPackets()
	if len(data) != 1 || data[0].Size != model.SEGMENT_SIZE {
		t.Fatalf("first data transmission = %+v", data)
	}
	assertTotal(0)

	// Sending and retransmitting the first segment do not confirm delivery.
	ack(0)
	ack(2)
	otherFlow := c.Key()
	otherFlow.SrcPort++
	c.HandlePacket(model.Packet{Key: otherFlow, Flag: model.FlagACK, Seq: 1, Ack: 1025})
	runUntil(time.Millisecond + model.SEGMENT_TIMEOUT)
	data = dataPackets()
	if len(data) != 2 || data[0] != data[1] {
		t.Fatalf("expected retransmission of the outstanding segment, got %+v", data)
	}
	assertTotal(0)
	ack(1025)
	assertTotal(model.SEGMENT_SIZE)
	if c.StreamCompleted() {
		t.Fatal("stream completed after only its first segment was acknowledged")
	}
	ack(1025)
	assertTotal(model.SEGMENT_SIZE)

	runUntil(e.Now() + time.Millisecond)
	ack(1025)
	ack(1 + streamBytes)
	assertTotal(model.SEGMENT_SIZE)
	ack(2049)
	assertTotal(2 * model.SEGMENT_SIZE)
	if c.StreamCompleted() {
		t.Fatal("stream completed before its partial final segment was acknowledged")
	}

	runUntil(e.Now() + time.Millisecond)
	data = dataPackets()
	if len(data) != 4 || data[3].Size != finalBytes {
		t.Fatalf("expected a %d-byte final segment, got %+v", finalBytes, data)
	}
	ack(2049)
	assertTotal(2 * model.SEGMENT_SIZE)
	ack(1 + streamBytes)
	assertTotal(streamBytes)
	ack(1 + streamBytes)
	if !c.StreamCompleted() || totalAtCompletion != streamBytes {
		t.Fatalf("completion saw %d acknowledged bytes; completed = %v", totalAtCompletion, c.StreamCompleted())
	}
	if !slices.Equal(increments, []int{model.SEGMENT_SIZE, model.SEGMENT_SIZE, finalBytes}) {
		t.Fatalf("payload callbacks = %v", increments)
	}
	if !slices.Equal(observedTotals, []uint64{model.SEGMENT_SIZE, 2 * model.SEGMENT_SIZE, streamBytes}) {
		t.Fatalf("payload totals visible in callbacks = %v", observedTotals)
	}

	runUntil(e.Now())
	c.HandlePacket(model.Packet{
		Key: c.Key(), Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 2 + streamBytes,
	})
	c.Shutdown()
	assertTotal(streamBytes)
	fresh := client.NewClient(e, "10.0.0.1", 1000, "10.0.0.2", 80, model.TCP)
	if got := fresh.GetState().AcknowledgedPayloadBytes; got != 0 {
		t.Fatalf("fresh client acknowledged payload = %d, want 0", got)
	}
}

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
			c.HandlePacket(model.Packet{Key: c.Key(), Flag: model.FlagSYN | model.FlagACK, Ack: 1})
			if err := e.RunUntil(0); err != nil {
				t.Fatal(err)
			}
			c.HandlePacket(model.Packet{Key: c.Key(), Flag: model.FlagACK, Seq: 1, Ack: 1025})
			if err := e.RunUntil(0); err != nil {
				t.Fatal(err)
			}
			if len(data) != 2 || data[1].Size != 23 {
				t.Fatalf("expected unacknowledged final data, got %+v", data)
			}
			if finAck {
				// It acknowledges the outstanding range, but FIN still closes the transfer.
				c.HandlePacket(model.Packet{
					Key: c.Key(), Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 1048,
				})
			} else {
				c.Shutdown()
			}
			c.HandlePacket(model.Packet{Key: c.Key(), Flag: model.FlagACK, Seq: 1, Ack: 1048})
			if got := c.GetState().AcknowledgedPayloadBytes; got != model.SEGMENT_SIZE {
				t.Fatalf("closed partial transfer acknowledged payload = %d, want %d", got, model.SEGMENT_SIZE)
			}
			if !c.IsTerminated() || c.StreamCompleted() {
				t.Fatalf("terminated = %v, completed = %v", c.IsTerminated(), c.StreamCompleted())
			}
		})
	}
}
