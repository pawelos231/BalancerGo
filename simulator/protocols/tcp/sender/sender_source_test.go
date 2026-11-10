package sender

import (
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// The transport tests supply bytes without importing a client workload implementation.
type testPayloadSource struct {
	remaining int
	gap       time.Duration
	endTime   time.Duration
	readyAt   time.Duration
	withinRun bool
}

func newTestSender(e *engine.Engine) *Sender {
	return NewSender(e, model.ClientKey{
		SrcIP: "10.0.0.1", SrcPort: 1000,
		DstIP: "10.0.0.2", DstPort: 80, Protocol: model.TCP,
	})
}

func newTestPayloadSource(e *engine.Engine, bytes int, gap time.Duration) *testPayloadSource {
	return &testPayloadSource{remaining: bytes, gap: gap, endTime: e.EndTime()}
}

func (source *testPayloadSource) Start(now time.Duration) {
	source.withinRun = source.gap <= source.endTime-now
	if source.withinRun {
		source.readyAt = now + source.gap
	}
}

func (source *testPayloadSource) RemainingBytes() int { return source.remaining }
func (source *testPayloadSource) NextSendAt() (time.Duration, bool) {
	return source.readyAt, source.withinRun
}
func (source *testPayloadSource) Sent(bytes int, now time.Duration) {
	source.remaining -= bytes
	source.Start(now)
}
