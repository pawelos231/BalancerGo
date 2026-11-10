package tcp_test

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	tcpreceiver "load-balancer/protocols/tcp/receiver"
)

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
	receiver := tcpreceiver.NewReceiver(e, tcpreceiver.ReceiverCallbacks{Send: sender.HandlePacket})
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
