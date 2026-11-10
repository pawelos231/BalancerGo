package tcp_test

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	tcpreceiver "load-balancer/protocols/tcp/receiver"
)

// TCP endpoints can transfer and close without a client workload or backend host package.
// A small source supplies 2500 bytes; the engine delivers each direction one millisecond later.
func TestTransportEndpointsRunWithoutHostImplementations(t *testing.T) {
	e := engine.New(time.Second)
	sender := newTestSender(e)
	closeConfig := model.DefaultCloseConfig()
	closeConfig.MSL = time.Millisecond
	sender.SetCloseConfig(closeConfig)
	sender.SetSACKEnabled(true)

	received := 0
	confirmed := 0
	receiver := tcpreceiver.NewReceiver(e, tcpreceiver.ReceiverCallbacks{
		Send: func(packet model.Packet) {
			e.ScheduleAfter(time.Millisecond, func() { sender.HandlePacket(packet) })
		},
		OnPayloadReceived: func(bytes int) { received += bytes },
	})
	receiver.SetCloseConfig(closeConfig)
	receiver.SetSACKEnabled(true)
	sender.SetSender(func(packet model.Packet) {
		e.ScheduleAfter(time.Millisecond, func() { receiver.HandlePacket(packet) })
	})
	sender.OnPayloadAcknowledged = func(bytes int) { confirmed += bytes }
	sender.SetSource(newTestPayloadSource(e, 2500, 10*time.Millisecond))
	sender.EstablishConnection()

	if err := e.RunUntil(e.EndTime()); err != nil {
		t.Fatal(err)
	}
	if received != 2500 || confirmed != 2500 || !sender.StreamCompleted() {
		t.Fatalf("transport payload received=%d, confirmed=%d, state=%+v",
			received, confirmed, sender.GetState())
	}
	if !sender.IsTerminated() || sender.GetConnectionState().Outcome != model.CloseGraceful ||
		receiver.GetConnectionState(sender.Key()).State != model.TCPClosed || e.Pending() != 0 {
		t.Fatal("standalone TCP endpoints did not finish closing and release their timers")
	}
}
