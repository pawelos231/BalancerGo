package network

import (
	"container/heap"
	"time"

	"load-balancer/engine"
)

// Egress means outgoing traffic: LB -> backends and LB -> clients.
// It controls all LB outputs in one place.
type Egress struct {
	engine                  *engine.Engine
	forwardingBitsPerSecond int64
	readyOutputs            readyOutputs
	nextSequence            uint64
	dispatching             bool

	nextGrantAt    time.Duration
	grantBeyondEnd bool
	wakeEvent      engine.EventID
	wakeAt         time.Duration

	stats               ForwardingStats
	budgetWaiting       bool
	budgetWaitStartedAt time.Duration
}

// ForwardingStats counts packets when transmission starts, including later losses.
// BudgetWait is virtual time with ready traffic held back by the shared limit.
type ForwardingStats struct {
	ForwardingBitsPerSecond int64
	ForwardedPackets        uint64
	ForwardedBytes          uint64
	QueuedPackets           int
	QueueDroppedPackets     uint64
	QueueDroppedBytes       uint64
	BudgetWait              time.Duration
	BudgetBlocked           bool
}

// egressPacket keeps one waiting packet and its stable admission order.
type egressPacket struct {
	sizeBytes      int
	onArrival      func()
	forwardingCost time.Duration
	queuedAt       time.Duration
	sequence       uint64
}

// Output is one LB exit, e.g. the path to backend 1.
// It holds that link and the queue of packets waiting to be sent through it.
type Output struct {
	egress     *Egress
	link       *Link
	queue      []egressPacket
	queueHead  int // Index of the first waiting packet.
	readyIndex int
	stats      Stats
}

// NewEgress creates a dispatcher with a positive shared limit and no idle events.
func NewEgress(e *engine.Engine, forwardingBitsPerSecond int64) *Egress {
	if e == nil {
		panic("network: egress engine must not be nil")
	}
	if forwardingBitsPerSecond <= 0 {
		panic("network: forwarding bandwidth must be positive bits per second")
	}

	return &Egress{
		engine:                  e,
		forwardingBitsPerSecond: forwardingBitsPerSecond,
		stats: ForwardingStats{
			ForwardingBitsPerSecond: forwardingBitsPerSecond,
		},
	}
}

// AddOutput registers an existing link, e.g. LB -> backend, as an LB output.
// It creates its queue and a callback to send the next packet when the link becomes free.
func (e *Egress) AddOutput(link *Link) *Output {
	if link == nil {
		panic("network: output link must not be nil")
	}

	if link.engine != e.engine {
		panic("network: output link must use the egress engine")
	}

	output := &Output{
		egress:     e,
		link:       link,
		readyIndex: -1,
	}
	link.useExternalQueue(func() {
		// Travel is scheduled; try dispatch now and wait for a grant only if needed.
		e.markReady(output)
		e.dispatch()
	})
	return output
}

// Transmit admits a packet or drops it when this output's waiting queue is full.
// Admission does not promise delivery before the experiment's end time.
func (output *Output) Transmit(sizeBytes int, onArrival func()) bool {
	// Validate before changing counters, including the serialization range.
	if sizeBytes < 0 {
		panic("network: packet size must not be negative")
	}
	if onArrival == nil {
		panic("network: arrival callback must not be nil")
	}
	serializationDuration(sizeBytes, output.link.config.BandwidthBitsPerSecond)
	forwardingCost := serializationDuration(sizeBytes, output.egress.forwardingBitsPerSecond)

	// An immediate transmission does not consume a waiting slot, even at zero.
	linkFree := output.link.Ready() && output.queued() == 0
	budgetFree := !output.egress.budgetUnavailable()
	noOlderReadyHeads := output.egress.readyOutputs.Len() == 0
	canStartNow := linkFree && budgetFree && noOlderReadyHeads && !output.egress.dispatching
	queueFull := output.queued() >= output.link.config.QueueCapacity
	accepted := canStartNow || !queueFull
	if accepted && output.egress.nextSequence == ^uint64(0) {
		panic("network: egress enqueue sequence overflow")
	}

	output.stats.OfferedPackets++
	output.stats.OfferedBytes += uint64(sizeBytes)
	if !accepted {
		output.stats.QueueDroppedPackets++
		output.stats.QueueDroppedBytes += uint64(sizeBytes)
		output.egress.stats.QueueDroppedPackets++
		output.egress.stats.QueueDroppedBytes += uint64(sizeBytes)
		return false
	}

	output.stats.AcceptedPackets++
	output.stats.AcceptedBytes += uint64(sizeBytes)
	output.egress.nextSequence++
	output.egress.stats.QueuedPackets++
	output.queue = append(output.queue, egressPacket{
		sizeBytes:      sizeBytes,
		onArrival:      onArrival,
		forwardingCost: forwardingCost,
		queuedAt:       output.egress.engine.Now(),
		sequence:       output.egress.nextSequence,
	})

	// Starting synchronously preserves the existing link event order.
	output.egress.markReady(output)
	output.egress.dispatch()
	return true
}

// Stats includes the current budget wait without changing counters or events.
func (e *Egress) Stats() ForwardingStats {
	stats := e.stats
	stats.BudgetWait += e.currentBudgetWait()
	stats.BudgetBlocked = e.budgetBlocked()
	return stats
}

// Stats combines this FIFO's admissions with the link's physical progress.
func (output *Output) Stats() Stats {
	stats := output.link.Stats()
	stats.OfferedPackets = output.stats.OfferedPackets
	stats.OfferedBytes = output.stats.OfferedBytes
	stats.AcceptedPackets = output.stats.AcceptedPackets
	stats.AcceptedBytes = output.stats.AcceptedBytes
	stats.QueueDroppedPackets = output.stats.QueueDroppedPackets
	stats.QueueDroppedBytes = output.stats.QueueDroppedBytes
	stats.QueuedPackets = output.queued()
	return stats
}

// queued counts waiting packets without the consumed prefix of the slice.
func (output *Output) queued() int {
	return len(output.queue) - output.queueHead
}

// markReady keeps exactly one heap entry for a ready output with a waiting head.
func (e *Egress) markReady(output *Output) {
	eligible := output.queued() > 0 && output.link.Ready()
	if eligible && output.readyIndex < 0 {
		heap.Push(&e.readyOutputs, output)
	}
	if !eligible && output.readyIndex >= 0 {
		heap.Remove(&e.readyOutputs, output.readyIndex)
	}
}

// dispatch starts the oldest ready heads without polling unrelated outputs.
func (e *Egress) dispatch() {
	// A nested readiness notification is handled by the current dispatch loop.
	if e.dispatching {
		return
	}

	e.dispatching = true
	e.accountBudgetWait()
	defer func() {
		e.accountBudgetWait()
		e.scheduleBudgetWake()
		e.dispatching = false
	}()

	for e.readyOutputs.Len() > 0 && !e.budgetUnavailable() {
		// Choose the ready output whose first waiting packet is the oldest.
		output := heap.Pop(&e.readyOutputs).(*Output)
		packet := output.queue[output.queueHead]

		// Start transmitting that packet on the output's link.
		if !output.link.TryStart(packet.sizeBytes, packet.onArrival) {
			panic("network: ready output could not start its transmission")
		}

		// Remove it from the FIFO; the link now handles its transmission and arrival.
		output.removeHead()
		e.stats.QueuedPackets--
		e.stats.ForwardedPackets++
		e.stats.ForwardedBytes += uint64(packet.sizeBytes)
		e.reserveBudget(packet.forwardingCost)
	}
}

// budgetUnavailable includes grants whose waiting time extends past the horizon.
func (e *Egress) budgetUnavailable() bool {
	return e.grantBeyondEnd || e.engine.Now() < e.nextGrantAt
}

// budgetBlocked requires a free output, so busy-link waiting is not budget wait.
func (e *Egress) budgetBlocked() bool {
	return e.readyOutputs.Len() > 0 && e.budgetUnavailable()
}

// reserveBudget runs alongside link serialization and never accumulates idle credit.
func (e *Egress) reserveBudget(cost time.Duration) {
	now := e.engine.Now()
	remaining := e.engine.EndTime() - now
	e.grantBeyondEnd = cost > remaining
	if e.grantBeyondEnd {
		// The flag keeps the budget blocked without adding an overflowing duration.
		e.nextGrantAt = e.engine.EndTime()
		return
	}
	e.nextGrantAt = now + cost
}

// currentBudgetWait measures one shared blocked interval, not one per output.
func (e *Egress) currentBudgetWait() time.Duration {
	if !e.budgetWaiting {
		return 0
	}

	until := e.engine.Now()
	if !e.grantBeyondEnd && until > e.nextGrantAt {
		until = e.nextGrantAt
	}
	if until <= e.budgetWaitStartedAt {
		return 0
	}
	return until - e.budgetWaitStartedAt
}

// accountBudgetWait closes the previous interval before tracking the current one.
func (e *Egress) accountBudgetWait() {
	e.stats.BudgetWait += e.currentBudgetWait()
	e.budgetWaiting = e.budgetBlocked()
	e.budgetWaitStartedAt = e.engine.Now()
}

// scheduleBudgetWake keeps one useful grant event and removes obsolete wakeups.
func (e *Egress) scheduleBudgetWake() {
	needsWake := e.budgetBlocked() && !e.grantBeyondEnd
	wakeAlreadyScheduled := e.wakeEvent != 0 && e.wakeAt == e.nextGrantAt
	if needsWake && wakeAlreadyScheduled {
		return
	}

	if e.wakeEvent != 0 {
		e.engine.Cancel(e.wakeEvent)
		e.wakeEvent = 0
	}
	if !needsWake {
		return
	}

	e.wakeAt = e.nextGrantAt
	e.wakeEvent = e.engine.ScheduleAt(e.wakeAt, func() {
		e.wakeEvent = 0
		e.dispatch()
	})
}

// removeHead releases a packet's callback and occasionally compacts the FIFO.
func (output *Output) removeHead() {
	const minConsumedSlots = 1024

	// Advance the head instead of copying all remaining packets on every removal.
	output.queue[output.queueHead] = egressPacket{}
	output.queueHead++
	remaining := output.queued()
	if remaining == 0 {
		output.queue = output.queue[:0]
		output.queueHead = 0
		return
	}

	// Reclaim a large consumed prefix without shifting the queue every packet.
	enoughConsumed := output.queueHead >= minConsumedSlots
	atLeastHalfConsumed := output.queueHead >= remaining
	if enoughConsumed && atLeastHalfConsumed {
		copy(output.queue, output.queue[output.queueHead:])
		clear(output.queue[remaining:])
		output.queue = output.queue[:remaining]
		output.queueHead = 0
	}
}

// readyOutputs orders eligible FIFO heads and tracks their heap positions.
type readyOutputs []*Output

func (outputs readyOutputs) Len() int {
	return len(outputs)
}

// Less orders older virtual admissions first, breaking ties by enqueue sequence.
func (outputs readyOutputs) Less(i, j int) bool {
	left := outputs[i].queue[outputs[i].queueHead]
	right := outputs[j].queue[outputs[j].queueHead]
	if left.queuedAt == right.queuedAt {
		return left.sequence < right.sequence
	}
	return left.queuedAt < right.queuedAt
}

// Swap keeps output positions valid while the heap reorders its entries.
func (outputs readyOutputs) Swap(i, j int) {
	outputs[i], outputs[j] = outputs[j], outputs[i]
	outputs[i].readyIndex = i
	outputs[j].readyIndex = j
}

// Push registers one eligible output at its new heap position.
func (outputs *readyOutputs) Push(value any) {
	output := value.(*Output)
	output.readyIndex = len(*outputs)
	*outputs = append(*outputs, output)
}

// Pop removes an output and releases the heap's reference to it.
func (outputs *readyOutputs) Pop() any {
	previous := *outputs
	last := len(previous) - 1
	output := previous[last]
	previous[last] = nil
	*outputs = previous[:last]
	output.readyIndex = -1
	return output
}
