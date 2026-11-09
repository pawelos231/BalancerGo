package network

import (
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
)

// Shrinking one shared configuration preserves both input FIFOs and their order.
func TestIngressReconfigureRetainsIndependentQueues(t *testing.T) {
	e := engine.New(30 * time.Millisecond)
	config := Config{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       10 * time.Millisecond,
		QueueCapacity:          2,
	}
	ingress := NewIngress(NewLink(e, config, nil))
	clients, backends := ingress.AddInput(), ingress.AddInput()
	var arrivals []string
	record := func(name string) func() {
		return func() { arrivals = append(arrivals, name+"@"+e.Now().String()) }
	}
	clients.Transmit(1, record("client-1"))
	backends.Transmit(1, record("backend-1"))
	clients.Transmit(1, record("client-2"))
	backends.Transmit(1, record("backend-2"))
	clients.Transmit(1, record("client-3"))

	before := ingress.Stats()
	config.BandwidthBitsPerSecond = 16_000
	config.PacketTravelTime = 20 * time.Millisecond
	config.QueueCapacity = 1
	update := ingress.PrepareConfig(config, nil)
	if ingress.Config() == config || ingress.Stats() != before {
		t.Fatal("preparing an edit changed the live ingress")
	}
	e.ScheduleConfigurationAt(500*time.Microsecond, update.Apply)
	runLinkUntil(t, e, 500*time.Microsecond)
	if ingress.Config() != config || ingress.Stats() != before {
		t.Fatal("the edit discarded queued packets or reset counters")
	}
	if clients.Transmit(1, record("dropped client")) || backends.Transmit(1, record("dropped backend")) {
		t.Fatal("both FIFOs must reject waiting work above their new capacity")
	}

	runLinkUntil(t, e, 2*time.Millisecond)
	if clients.Stats().QueuedPackets != 1 || backends.Stats().QueuedPackets != 0 {
		t.Fatal("queued work lost its global admission order")
	}
	if !backends.Transmit(1, record("backend-3")) {
		t.Fatal("the backend FIFO must admit work when its own slot opens")
	}
	if clients.Transmit(1, record("still dropped client")) {
		t.Fatal("the client's full FIFO must remain full")
	}

	// Once all packets are travelling, later edits cannot change their arrivals.
	runLinkUntil(t, e, 4*time.Millisecond)
	config.PacketTravelTime = 0
	config.LossRate = 1
	ingress.PrepareConfig(config, nil).Apply()
	runLinkUntil(t, e, e.EndTime())

	want := []string{
		"client-1@11ms", "backend-1@21.5ms", "client-2@22ms",
		"backend-2@22.5ms", "client-3@23ms", "backend-3@23.5ms",
	}
	if !reflect.DeepEqual(arrivals, want) {
		t.Fatalf("arrivals = %v, want %v", arrivals, want)
	}
	stats := ingress.Stats()
	if stats.AcceptedPackets != 6 || stats.DeliveredPackets != 6 ||
		stats.QueueDroppedPackets != 3 || stats.LostPackets != 0 || stats.QueuedPackets != 0 {
		t.Fatalf("reconfiguration changed historical counters or admitted work: %+v", stats)
	}
	if clients.Stats().DeliveredPackets != 3 || backends.Stats().DeliveredPackets != 3 {
		t.Fatal("physical deliveries must retain their input attribution")
	}
}

// New loss settings affect the next input's packet, while active service stays frozen.
func TestIngressReconfigureCapturesLossAtDispatch(t *testing.T) {
	e := engine.New(3 * time.Millisecond)
	config := Config{BandwidthBitsPerSecond: 8_000, QueueCapacity: 1}
	ingress := NewIngress(NewLink(e, config, nil))
	clients, backends := ingress.AddInput(), ingress.AddInput()
	arrivals := 0
	clients.Transmit(1, func() { arrivals++ })
	backends.Transmit(1, func() { t.Error("the queued packet must use the new loss rate") })

	config.LossRate = 1
	update := ingress.PrepareConfig(config, nil)
	e.ScheduleConfigurationAt(500*time.Microsecond, update.Apply)
	runLinkUntil(t, e, e.EndTime())
	if arrivals != 1 || clients.Stats().DeliveredPackets != 1 || backends.Stats().LostPackets != 1 {
		t.Fatal("the loss edit changed active work or lost input attribution")
	}
	if stats := ingress.Stats(); stats.LostPackets != 1 || stats.TransmittedBytes != 2 {
		t.Fatalf("the shared link must count both transmissions once: %+v", stats)
	}
}

// Queue-policy edits apply at admission, without changing already queued CE state.
func TestIngressReconfigureSignalsOnlyNewAdmissions(t *testing.T) {
	e := engine.New(5 * time.Millisecond)
	config := Config{BandwidthBitsPerSecond: 8_000, QueueCapacity: 4}
	ingress := NewIngress(NewLink(e, config, nil))
	clients, backends := ingress.AddInput(), ingress.AddInput()
	var marks []bool
	record := func(marked bool) { marks = append(marks, marked) }
	clients.TransmitECN(1, Capable, record)
	backends.TransmitECN(1, Capable, record)

	config.QueuePolicy = "threshold"
	config.CongestionSignalProbability = 1
	ingress.PrepareConfig(config, nil).Apply()
	if !clients.TransmitECN(1, Capable, record) {
		t.Fatal("ECN-capable waiting work must be admitted with a mark")
	}
	if backends.Transmit(1, func() { t.Error("early-dropped packet arrived") }) {
		t.Fatal("new non-ECN traffic must use the enabled early-drop policy")
	}

	config.QueuePolicy = "tailDrop"
	ingress.PrepareConfig(config, nil).Apply()
	if !backends.TransmitECN(1, Capable, record) {
		t.Fatal("disabling signaling must restore ordinary FIFO admission")
	}
	runLinkUntil(t, e, e.EndTime())
	if !reflect.DeepEqual(marks, []bool{false, false, true, false}) {
		t.Fatalf("a policy edit changed previously admitted packets: %v", marks)
	}
	stats := ingress.Stats()
	if stats.ECNMarkedPackets != 1 || stats.CongestionDroppedPackets != 1 ||
		stats.QueueDroppedPackets != 1 || stats.DeliveredPackets != 4 {
		t.Fatalf("policy edits reset counters or changed admitted work: %+v", stats)
	}
}
