package tcp

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// The host-independent exchange delivers each direction one virtual millisecond later.
// A test can drop one selected data range before starting the connection.
type mssExchange struct {
	engine   *engine.Engine
	sender   *Sender
	receiver *Receiver

	payloadBytes int
	received     int
	confirmed    int
	dataSizes    []int
	dropSeq      uint64
	dropped      bool
}

func newMSSExchange(t *testing.T, mssBytes, payloadBytes int) *mssExchange {
	t.Helper()
	test := &mssExchange{
		engine:       engine.New(10 * time.Second),
		payloadBytes: payloadBytes,
	}
	test.sender = newTestSender(test.engine)
	t.Cleanup(test.sender.Shutdown)

	// Hold one segment in flight so ordinary packet sizes are not split by cwnd.
	congestion := model.DefaultCongestionConfig()
	congestion.InitialWindowSegments = 1
	congestion.MaximumWindowSegments = 1
	congestion.InitialSlowStartThresholdBytes = max(
		congestion.InitialSlowStartThresholdBytes,
		uint64(2*mssBytes),
	)
	test.sender.SetCongestionConfigWithMSS(congestion, mssBytes)
	test.sender.SetSource(newTestPayloadSource(test.engine, payloadBytes, 0))
	test.sender.OnPayloadAcknowledged = func(bytes int) { test.confirmed += bytes }

	closeConfig := model.DefaultCloseConfig()
	closeConfig.MSL = time.Millisecond
	test.sender.SetCloseConfig(closeConfig)
	test.receiver = NewReceiver(test.engine, ReceiverCallbacks{
		Send: func(packet model.Packet) {
			test.engine.ScheduleAfter(time.Millisecond, func() {
				test.sender.HandlePacket(packet)
			})
		},
		OnPayloadReceived: func(bytes int) { test.received += bytes },
	})
	t.Cleanup(test.receiver.Shutdown)
	test.receiver.SetCloseConfig(closeConfig)
	test.receiver.SetIdleTimeouts(0, 0)
	test.sender.SetSender(func(packet model.Packet) {
		if packet.WireSize() > 65535 {
			t.Fatalf("packet exceeded the IPv4 size limit: %+v", packet)
		}
		if packet.Size > 0 {
			test.dataSizes = append(test.dataSizes, packet.Size)
			if packet.Seq == test.dropSeq && !test.dropped {
				test.dropped = true
				return
			}
		}
		test.engine.ScheduleAfter(time.Millisecond, func() {
			test.receiver.HandlePacket(packet)
		})
	})
	return test
}

func (test *mssExchange) run(t *testing.T) {
	t.Helper()
	test.sender.EstablishConnection()
	if err := test.engine.RunUntil(test.engine.EndTime()); err != nil {
		t.Fatal(err)
	}
	if !test.sender.StreamCompleted() {
		t.Fatalf("transfer did not complete: %+v", test.sender.GetTransferState())
	}
	if test.received != test.payloadBytes || test.confirmed != test.payloadBytes {
		t.Fatalf("payload counted incorrectly: received=%d confirmed=%d want=%d",
			test.received, test.confirmed, test.payloadBytes)
	}
}

// At MSS=1460, 2933 payload bytes become 1460 + 1460 + 13.
// The requested profiles and both numeric boundaries keep that exact byte total.
func TestManualMSSSplitsPayloadWithoutChangingItsTotal(t *testing.T) {
	for _, mssBytes := range []int{1, 536, 1024, 1452, 1460, 8960, model.MaxMSSBytes} {
		t.Run(fmt.Sprintf("mss-%d", mssBytes), func(t *testing.T) {
			tail := min(13, mssBytes)
			test := newMSSExchange(t, mssBytes, 2*mssBytes+tail)
			test.run(t)

			want := []int{mssBytes, mssBytes, tail}
			if !slices.Equal(test.dataSizes, want) {
				t.Fatalf("payload sizes=%v, want %v", test.dataSizes, want)
			}
			if retries := test.sender.GetTransferState().DataRetransmissions; retries != 0 {
				t.Fatalf("loss-free packets were retried %d times", retries)
			}
		})
	}
}

// rwnd can be smaller than MSS: a 512-byte window sends 1025 bytes as 512 + 512 + 1.
func TestManualMSSRespectsSmallerReceiveWindow(t *testing.T) {
	test := newMSSExchange(t, 8960, 1025)
	test.receiver.SetReceiveBufferBytes(512)
	test.run(t)

	if want := []int{512, 512, 1}; !slices.Equal(test.dataSizes, want) {
		t.Fatalf("payload sizes=%v, want %v", test.dataSizes, want)
	}
}

// Drop the third data segment once, then repair it with the selected MSS.
// Delayed ACK is active; supported algorithms also use negotiated SACK.
func TestManualMSSRepairsLossAcrossCongestionAlgorithms(t *testing.T) {
	for _, mode := range []model.CongestionMode{
		model.CongestionReno,
		model.CongestionNewReno,
		model.CongestionTahoe,
		model.CongestionCubic,
	} {
		for _, mssBytes := range []int{536, 8960} {
			name := fmt.Sprintf("%s/mss-%d", mode, mssBytes)
			t.Run(name, func(t *testing.T) {
				test := newMSSExchange(t, mssBytes, 16*mssBytes+17)

				// Several initial segments leave room for out-of-order loss reports.
				congestion := model.DefaultCongestionConfig()
				congestion.Mode = mode
				congestion.InitialWindowSegments = 8
				test.sender.SetCongestionConfig(congestion)
				test.dropSeq = 1 + uint64(2*mssBytes)

				useSACK := mode != model.CongestionTahoe
				test.sender.SetSACKEnabled(useSACK)
				test.receiver.SetSACKEnabled(useSACK)
				ack := model.DefaultDelayedACKConfig()
				ack.Enabled = true
				test.receiver.SetDelayedACKConfig(ack)
				test.run(t)

				if !test.dropped {
					t.Fatal("the intended third segment was not dropped")
				}
				retries := test.sender.GetTransferState().DataRetransmissions
				if retries < 1 || retries > 4 {
					t.Fatalf("one lost segment caused %d retransmissions", retries)
				}
				for _, size := range test.dataSizes {
					if size < 1 || size > mssBytes {
						t.Fatalf("sent payload %d exceeds selected MSS %d", size, mssBytes)
					}
				}
			})
		}
	}
}

// Once SYN startup begins, changing MSS would also change the live congestion window.
func TestManualMSSCannotChangeAfterConnectionStarts(t *testing.T) {
	test := newMSSExchange(t, 1460, 3000)
	test.sender.EstablishConnection()

	defer func() {
		if recover() == nil {
			t.Fatal("MSS changed after the connection started")
		}
	}()
	test.sender.SetMSSBytes(536)
}
