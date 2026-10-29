package client_test

import (
	"math"
	"testing"
	"time"

	"load-balancer/model"
)

// Byte positions must advance by payload length while stop-and-wait stays intact.
func TestTCPByteSequencesAndStopWait(t *testing.T) {
	for _, peerInitial := range []uint64{5000, 1<<32 + 5000} {
		t.Run(sequenceCaseName(peerInitial), func(t *testing.T) {
			h := newTransferHarness(t)
			h.client.SendCompleteStream(2050, 0)
			h.client.EstablishConnection()
			h.runUntil(0)
			if len(h.sent) != 1 || h.sent[0].Flag != model.FlagSYN ||
				h.sent[0].Seq != 0 || h.sent[0].Ack != 0 || h.sent[0].Size != 0 {
				t.Fatalf("initial SYN = %+v", h.sent)
			}

			h.client.HandlePacket(model.Packet{
				ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
				Key:                h.client.Key(), Flag: model.FlagSYN | model.FlagACK,
				Seq: peerInitial, Ack: 1,
			})
			if got := h.sent[1]; got.Flag != model.FlagACK || got.Seq != 1 ||
				got.Ack != peerInitial+1 || got.Size != 0 {
				t.Fatalf("handshake ACK = %+v", got)
			}
			h.runUntil(400 * time.Millisecond)

			segments := []struct {
				sequence uint64
				size     int
				ack      uint64
			}{
				{sequence: 1, size: 1024, ack: 1025},
				{sequence: 1025, size: 1024, ack: 2049},
				{sequence: 2049, size: 2, ack: 2051},
			}
			for index, expected := range segments {
				data := sentClientData(h.sent)
				if len(data) != index+1 {
					t.Fatalf("data count before ACK %d = %d, want %d", index, len(data), index+1)
				}
				packet := data[index]
				if packet.Seq != expected.sequence || packet.Ack != peerInitial+1 ||
					packet.Size != expected.size || packet.WireSize() != expected.size+40 {
					t.Fatalf("data segment %d = %+v", index, packet)
				}
				h.client.HandlePacket(model.Packet{
					ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
					Key:                h.client.Key(), Flag: model.FlagACK,
					Seq: peerInitial + 1, Ack: expected.ack,
				})
				h.runUntil(h.engine.Now())
			}

			// FIN consumes one position, but contributes no payload bytes.
			fin := h.sent[len(h.sent)-1]
			if fin.Flag != model.FlagFIN|model.FlagACK || fin.Seq != 2051 ||
				fin.Ack != peerInitial+1 || fin.Size != 0 {
				t.Fatalf("closing FIN = %+v", fin)
			}
			h.client.HandlePacket(model.Packet{
				ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
				Key:                h.client.Key(), Flag: model.FlagFIN | model.FlagACK,
				Seq: peerInitial + 1, Ack: 2052,
			})
			if !h.client.IsTerminated() || !h.client.StreamCompleted() ||
				h.client.GetState().AcknowledgedPayloadBytes != 2050 {
				t.Fatalf("completed client = %+v", h.client.GetState())
			}
		})
	}
}

func TestTCPRejectsInvalidHandshakeNumbers(t *testing.T) {
	tests := []struct {
		name   string
		packet model.Packet
	}{
		{"unacknowledged SYN", model.Packet{Flag: model.FlagSYN | model.FlagACK, Ack: 0}},
		{"future SYN ACK", model.Packet{Flag: model.FlagSYN | model.FlagACK, Ack: 2}},
		{"SYN payload", model.Packet{Flag: model.FlagSYN | model.FlagACK, Ack: 1, Size: 1}},
		{"SYN and FIN", model.Packet{Flag: model.FlagSYN | model.FlagACK | model.FlagFIN, Ack: 1}},
		{"sequence overflow", model.Packet{Flag: model.FlagSYN | model.FlagACK, Seq: math.MaxUint64, Ack: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newTransferHarness(t)
			h.client.EstablishConnection()
			h.runUntil(0)
			packet := test.packet
			packet.Key = h.client.Key()
			packet.ReceiveWindowBytes = model.DefaultReceiveBufferBytes
			h.client.HandlePacket(packet)
			if h.client.IsHandshakeCompleted() || h.client.IsTerminated() || len(h.sent) != 1 {
				t.Fatalf("invalid handshake changed client: state=%+v, sent=%+v", h.client.GetState(), h.sent)
			}
		})
	}

	t.Run("unsolicited SYN ACK", func(t *testing.T) {
		h := newTransferHarness(t)
		h.client.HandlePacket(model.Packet{
			ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
			Key:                h.client.Key(), Flag: model.FlagSYN | model.FlagACK, Ack: 1,
		})
		if h.client.IsHandshakeCompleted() || len(h.sent) != 0 {
			t.Fatal("an unsolicited SYN ACK established a connection")
		}
	})
}

func TestTCPSYNRetryKeepsItsOriginalSequence(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(1, 0)
	h.client.EstablishConnection()
	h.runUntil(2 * model.SYN_TIMEOUT)
	if len(h.sent) != 3 {
		t.Fatalf("SYN transmissions = %d, want original plus two retries", len(h.sent))
	}
	for _, packet := range h.sent {
		if packet != h.sent[0] || packet.Flag != model.FlagSYN ||
			packet.Seq != 0 || packet.Ack != 0 || packet.Size != 0 {
			t.Fatalf("SYN retry changed its reserved position: %+v", h.sent)
		}
	}

	// Retries reuse SYN's position; the first payload still starts at byte 1.
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(h.engine.Now())
	data := sentClientData(h.sent)
	if len(data) != 1 || data[0].Seq != 1 || data[0].Ack != 1 || data[0].Size != 1 {
		t.Fatalf("first payload after SYN retries = %+v", data)
	}
}

func TestTCPDataAcknowledgmentsMatchOutstandingByteRange(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(17, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	original := sentClientData(h.sent)[0]

	// Duplicate or invalid ACKs cannot confirm any bytes of [1, 18).
	invalid := []model.Packet{
		{Flag: model.FlagACK, Seq: 1, Ack: 0},
		{Flag: model.FlagACK, Seq: 1, Ack: 1},
		{Flag: model.FlagACK, Seq: 1, Ack: 19},
		{Flag: model.FlagACK, Seq: 2, Ack: 18},
		{Seq: 1, Ack: 18},
		{Flag: model.FlagACK, Seq: 1, Ack: 18, Size: 1},
	}
	for _, packet := range invalid {
		packet.Key = h.client.Key()
		packet.ReceiveWindowBytes = model.DefaultReceiveBufferBytes
		h.client.HandlePacket(packet)
		if h.client.StreamCompleted() || h.client.GetState().AcknowledgedPayloadBytes != 0 {
			t.Fatalf("invalid ACK confirmed payload: %+v", packet)
		}
	}

	// Invalid ACKs leave the original range outstanding for its existing timer.
	h.runUntil(model.SEGMENT_TIMEOUT)
	data := sentClientData(h.sent)
	if len(data) != 2 || data[1] != original {
		t.Fatalf("byte-range retransmission = %+v", data)
	}
	h.receive(model.FlagACK, 18)
	h.receive(model.FlagACK, 18)
	if !h.client.StreamCompleted() || h.client.GetState().AcknowledgedPayloadBytes != 17 {
		t.Fatalf("valid ACK did not confirm exactly 17 bytes: %+v", h.client.GetState())
	}
}

func TestTCPDuplicateHandshakeDoesNotResetOutstandingData(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(2048, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	original := sentClientData(h.sent)[0]
	h.runUntil(300 * time.Millisecond)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	if len(sentClientData(h.sent)) != 1 {
		t.Fatal("duplicate SYN ACK released another data segment")
	}
	h.runUntil(model.SEGMENT_TIMEOUT)
	data := sentClientData(h.sent)
	if len(data) != 2 || data[1] != original {
		t.Fatalf("duplicate handshake changed the outstanding range or timer: %+v", data)
	}

	// A changed peer sequence cannot rewrite an established receive position.
	before := len(h.sent)
	h.client.HandlePacket(model.Packet{
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
		Key:                h.client.Key(), Flag: model.FlagSYN | model.FlagACK, Seq: 99, Ack: 1,
	})
	if len(h.sent) != before {
		t.Fatal("a different peer SYN sequence was accepted after establishment")
	}
	h.receive(model.FlagACK, 1025)
	h.runUntil(h.engine.Now())
	data = sentClientData(h.sent)
	if len(data) != 3 || data[2].Seq != 1025 || data[2].Ack != 1 {
		t.Fatalf("next data range after duplicate handshake = %+v", data)
	}
}

func TestTCPEmptyTransferUsesSYNAndFINSequencePositions(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	fin := h.sent[len(h.sent)-1]
	if fin.Flag != model.FlagFIN|model.FlagACK || fin.Seq != 1 || fin.Ack != 1 || fin.Size != 0 {
		t.Fatalf("empty transfer FIN = %+v", fin)
	}
	for _, packet := range []model.Packet{
		{Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 1},
		{Flag: model.FlagFIN | model.FlagACK, Seq: 2, Ack: 2},
	} {
		packet.Key = h.client.Key()
		packet.ReceiveWindowBytes = model.DefaultReceiveBufferBytes
		h.client.HandlePacket(packet)
		if h.client.IsTerminated() {
			t.Fatalf("invalid FIN ACK closed the connection: %+v", packet)
		}
	}
	h.receive(model.FlagFIN|model.FlagACK, 2)
	if !h.client.IsTerminated() || h.client.GetState().AcknowledgedPayloadBytes != 0 ||
		h.transfer().Status != model.TransferSucceeded {
		t.Fatalf("empty transfer outcome = %+v", h.client.GetState())
	}
}

func sentClientData(packets []model.Packet) []model.Packet {
	var data []model.Packet
	for _, packet := range packets {
		if packet.Size > 0 {
			data = append(data, packet)
		}
	}
	return data
}

func sequenceCaseName(initial uint64) string {
	if initial > math.MaxUint32 {
		return "peer position beyond 32 bits"
	}
	return "ordinary peer position"
}
