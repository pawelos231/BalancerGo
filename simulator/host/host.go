package host

import (
	"time"

	"load-balancer/engine"
)

// Host processes callbacks on a configurable pool of virtual cores.
// All calls belong to the engine's owner; modeled cores do not create goroutines.
type Host struct {
	engine *engine.Engine
	config Config
	stats  Stats

	queue     []packetWork
	queueHead int
	active    map[*packetWork]struct{}
	stopped   bool
	pumping   bool
	pumpEvent engine.EventID

	createdAt   time.Duration
	accountedAt time.Duration
	stoppedAt   time.Duration
}

type packetWork struct {
	queuedAt    time.Duration
	onProcessed func()
	completion  engine.EventID
}

// New creates an idle host. Cost zero keeps the old immediate callback path.
func New(e *engine.Engine, config Config) *Host {
	if e == nil {
		panic("host: engine must not be nil")
	}
	if err := config.Validate(e.EndTime() - e.Now()); err != nil {
		panic("host: " + err.Error())
	}
	return &Host{
		engine:      e,
		config:      config,
		active:      make(map[*packetWork]struct{}),
		createdAt:   e.Now(),
		accountedAt: e.Now(),
	}
}

// Submit accepts work on a free core or in the waiting FIFO.
// For example, two cores and two waiting slots can admit four simultaneous packets.
func (h *Host) Submit(onProcessed func()) bool {
	if onProcessed == nil {
		panic("host: processed callback must not be nil")
	}
	if h.stopped {
		return false
	}
	// A same-time arrival may precede a pending pump after capacity grows.
	// Existing waiters must take the available capacity first.
	h.pump()
	if h.stopped {
		return false
	}
	h.stats.OfferedPackets++

	// Bypass avoids adding zero-time events or changing existing packet ordering.
	if h.config.PacketProcessingTime == 0 && !h.pumping {
		h.stats.AcceptedPackets++
		h.bypass(packetWork{queuedAt: h.engine.Now(), onProcessed: onProcessed})
		return true
	}

	work := packetWork{queuedAt: h.engine.Now(), onProcessed: onProcessed}
	if h.queued() == 0 && len(h.active) < h.config.Cores && !h.pumping {
		h.stats.AcceptedPackets++
		h.start(work)
		return true
	}
	if h.config.PacketProcessingTime > 0 && h.queued() >= h.config.QueueCapacity {
		h.stats.QueueDroppedPackets++
		return false
	}

	h.stats.AcceptedPackets++
	h.queue = append(h.queue, work)
	h.stats.MaxQueuedPackets = max(h.stats.MaxQueuedPackets, h.queued())
	return true
}

// start reserves a core before scheduling the packet's completion.
func (h *Host) start(work packetWork) {
	h.accountBusyTime()
	h.accountStart(work)
	h.active[&work] = struct{}{}

	// A zero event ID means this work remains in service beyond the experiment end.
	work.completion = h.engine.ScheduleAfter(h.config.PacketProcessingTime, func() {
		h.complete(&work)
	})
}

// complete releases the core and reserves it for the oldest waiter before calling outside code.
func (h *Host) complete(work *packetWork) {
	if _, exists := h.active[work]; !exists {
		return
	}
	h.accountBusyTime()
	delete(h.active, work)
	h.stats.ProcessedPackets++
	onProcessed := work.onProcessed
	work.onProcessed = nil
	work.completion = 0

	// A callback may submit another packet or stop this host. FIFO waiters get first use.
	h.pump()
	onProcessed()
}

func (h *Host) accountStart(work packetWork) {
	h.stats.StartedPackets++
	wait := h.engine.Now() - work.queuedAt
	h.stats.QueueWaitTotal += wait
	h.stats.MaxQueueWait = max(h.stats.MaxQueueWait, wait)
}

func (h *Host) bypass(work packetWork) {
	h.accountStart(work)
	h.stats.ProcessedPackets++
	work.onProcessed()
}

// pump uses current settings only for work that has not started yet.
// Reentrant submissions join the FIFO while bypass callbacks are being flushed.
func (h *Host) pump() {
	if h.stopped || h.pumping {
		return
	}
	h.pumping = true
	defer func() { h.pumping = false }()
	for !h.stopped && h.queued() > 0 {
		if h.config.PacketProcessingTime == 0 {
			h.bypass(h.pop())
			continue
		}
		if len(h.active) >= h.config.Cores {
			return
		}
		h.start(h.pop())
	}
}

func (h *Host) queued() int {
	return len(h.queue) - h.queueHead
}

// pop advances the head instead of moving every remaining packet on each removal.
func (h *Host) pop() packetWork {
	const minConsumedSlots = 1024

	work := h.queue[h.queueHead]
	h.queue[h.queueHead] = packetWork{}
	h.queueHead++
	remaining := h.queued()
	if remaining == 0 {
		h.queue = h.queue[:0]
		h.queueHead = 0
		return work
	}

	// Reclaim a large consumed prefix instead of shifting on every packet.
	enoughConsumed := h.queueHead >= minConsumedSlots
	atLeastHalfConsumed := h.queueHead >= remaining
	if enoughConsumed && atLeastHalfConsumed {
		copy(h.queue, h.queue[h.queueHead:])
		clear(h.queue[remaining:])
		h.queue = h.queue[:remaining]
		h.queueHead = 0
	}
	return work
}

// accountBusyTime closes the interval before the number of occupied cores changes.
func (h *Host) accountBusyTime() {
	now := h.engine.Now()
	elapsed := now - h.accountedAt
	h.stats.BusyTime += elapsed * time.Duration(len(h.active))
	h.stats.AvailableCoreTime += elapsed * time.Duration(max(h.config.Cores, len(h.active)))
	h.accountedAt = now
}

// Stats reads partial active work without adding events or advancing the simulation.
func (h *Host) Stats() Stats {
	stats := h.stats
	now := h.engine.Now()
	if h.stopped {
		now = h.stoppedAt
	}
	stats.Enabled = h.config.PacketProcessingTime > 0
	stats.Stopped = h.stopped
	stats.Cores = h.config.Cores
	stats.PacketProcessingTime = h.config.PacketProcessingTime
	stats.BusyCores = len(h.active)
	stats.QueueCapacity = h.config.QueueCapacity
	stats.QueuedPackets = h.queued()
	stats.ObservedTime = now - h.createdAt
	stats.BusyTime += (now - h.accountedAt) * time.Duration(stats.BusyCores)
	stats.AvailableCoreTime += (now - h.accountedAt) * time.Duration(max(stats.Cores, stats.BusyCores))
	if stats.QueuedPackets > 0 {
		stats.MaxQueueWait = max(stats.MaxQueueWait, now-h.queue[h.queueHead].queuedAt)
	}
	return stats
}

// DiscardPending abandons accepted work but leaves the resource available for recovery.
// Queued and active packets are counted separately from new packets rejected by a full FIFO.
func (h *Host) DiscardPending() {
	if h.stopped {
		return
	}
	h.accountBusyTime()
	h.stats.MaxQueueWait = h.Stats().MaxQueueWait
	h.stats.DiscardedPackets += uint64(len(h.active) + h.queued())
	h.engine.Cancel(h.pumpEvent)
	h.pumpEvent = 0
	for work := range h.active {
		h.engine.Cancel(work.completion)
		work.onProcessed = nil
		delete(h.active, work)
	}
	clear(h.queue)
	h.queue = nil
	h.queueHead = 0
}

// Stop cancels pending CPU work and permanently rejects further submissions.
func (h *Host) Stop() {
	if h.stopped {
		return
	}
	h.DiscardPending()
	h.stopped = true
	h.stoppedAt = h.engine.Now()
}
