package tcp

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// A host can stop TCP from a callback, including while SYN, data or FIN is being processed.
// No later step may accept payload or create a new timer for the removed connection.
func TestReceiverCallbacksCanStopTheEndpoint(t *testing.T) {
	for _, phase := range []string{"admission", "halfOpen", "establishAdmission", "established", "peerFIN"} {
		t.Run(phase, func(t *testing.T) {
			e := engine.New(10 * time.Second)
			var receiver *Receiver
			receiver = NewReceiver(e, ReceiverCallbacks{
				CanOpen: func() bool {
					if phase == "admission" {
						receiver.Shutdown()
					}
					return true
				},
				CanEstablish: func() bool {
					if phase == "establishAdmission" {
						receiver.Shutdown()
					}
					return true
				},
				OnConnectionCounts: func(active, halfOpen uint32) {
					if phase == "halfOpen" && halfOpen > 0 || phase == "established" && active > 0 {
						receiver.Shutdown()
					}
				},
				OnPeerFIN: func(time.Duration) {
					if phase == "peerFIN" {
						receiver.Shutdown()
					}
				},
			})
			key := model.ClientKey{SrcIP: "10.0.0.1", DstIP: "10.0.0.2", Protocol: model.TCP}
			receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagSYN})
			payloadBytes := 10
			if phase == "peerFIN" {
				payloadBytes = 0
			}
			// Data can complete the handshake, so its admission callback runs before delivery.
			receiver.HandlePacket(model.Packet{
				Key: key, Flag: model.FlagACK, Seq: 1, Ack: 1, Size: payloadBytes,
			})
			if phase == "peerFIN" {
				receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 1})
			}
			if !receiver.stopped || receiver.ReceivedPayloadBytes() != 0 || e.Pending() != 0 ||
				receiver.GetConnectionState(key).State != model.TCPClosed {
				t.Fatalf("callback shutdown left live work: stopped=%v, bytes=%d, pending=%d",
					receiver.stopped, receiver.ReceivedPayloadBytes(), e.Pending())
			}
		})
	}
}

// Admission can replace the old connection before refusing that old handshake.
// Cleanup must leave the replacement SYN and its own timeout intact.
func TestReceiverAdmissionDoesNotRemoveAReplacementConnection(t *testing.T) {
	e := engine.New(10 * time.Second)
	key := model.ClientKey{SrcIP: "10.0.0.1", DstIP: "10.0.0.2", Protocol: model.TCP}
	var receiver *Receiver
	receiver = NewReceiver(e, ReceiverCallbacks{
		CanEstablish: func() bool {
			receiver.ForgetConnection(key)
			receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagSYN, Seq: 100})
			return false
		},
	})
	defer receiver.Shutdown()
	receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagSYN})
	receiver.HandlePacket(model.Packet{Key: key, Flag: model.FlagACK, Seq: 1, Ack: 1})
	if receiver.GetConnectionState(key).State != model.TCPSynReceived ||
		receiver.halfOpenConnections != 1 || receiver.activeConnections != 0 || e.Pending() != 1 {
		t.Fatalf("old admission removed the replacement: state=%+v, halfOpen=%d, pending=%d",
			receiver.GetConnectionState(key), receiver.halfOpenConnections, e.Pending())
	}
}
