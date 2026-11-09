package engine

import (
	"container/heap"
	"errors"
	"fmt"
	"time"
)

// EventID identifies one scheduled callback
type EventID uint64

const DefaultMaxEvents uint64 = 5_000_000_000

var (
	ErrEventLimit   = errors.New("engine: event execution limit reached")
	ErrReentrantRun = errors.New("engine: RunUntil cannot be called from a running callback")
)

// Engine owns the future-event queue. Configuration runs before ordinary work at
// the same time; events within either kind follow scheduling order (EventID).
type Engine struct {
	// MaxEvents bounds the total number of callbacks executed over all RunUntil
	MaxEvents      uint64
	now            time.Duration
	endTime        time.Duration // virtual end time; no events are scheduled after it.
	nextID         EventID
	queue          eventQueue
	byID           map[EventID]*event
	executed       uint64
	running        bool
	lastOrdinaryAt time.Duration // -1 until an ordinary callback starts.
}

// New creates a simulation engine with its clock starting at zero.
func New(endTime time.Duration) *Engine {
	if endTime < 0 {
		panic("engine: end time must not be negative")
	}
	return &Engine{
		endTime:        endTime,
		MaxEvents:      DefaultMaxEvents,
		byID:           make(map[EventID]*event),
		lastOrdinaryAt: -1,
	}
}

func (e *Engine) Now() time.Duration {
	return e.now
}

func (e *Engine) EndTime() time.Duration {
	return e.endTime
}

func (e *Engine) Pending() int {
	return len(e.queue)
}

// NextEventTime reads the next timestamp without removing or executing its event.
func (e *Engine) NextEventTime() (time.Duration, bool) {
	if len(e.queue) == 0 {
		return 0, false
	}
	return e.queue[0].at, true
}

func (e *Engine) EventsExecuted() uint64 {
	return e.executed
}

// Clear discards pending callbacks when the whole simulation is stopped.
func (e *Engine) Clear() {
	for _, item := range e.queue {
		item.callback = nil
	}
	e.queue = nil
	clear(e.byID)
}

// ScheduleAt schedules a callback, or returns zero when at exceeds EndTime.
func (e *Engine) ScheduleAt(at time.Duration, callback func()) EventID {
	return e.scheduleAt(at, ordinaryEvent, callback)
}

// ScheduleConfigurationAt runs configuration before ordinary events at the same time.
// It cannot insert configuration after ordinary work at that timestamp has started.
func (e *Engine) ScheduleConfigurationAt(at time.Duration, callback func()) EventID {
	return e.scheduleAt(at, configurationEvent, callback)
}

// scheduleAt shares queue limits and cancellation bookkeeping for both event kinds.
func (e *Engine) scheduleAt(at time.Duration, priority eventPriority, callback func()) EventID {
	if callback == nil {
		panic("engine: callback must not be nil")
	}

	if at < 0 || at < e.now {
		panic("engine: cannot schedule an event in the past")
	}

	if at > e.endTime {
		return 0
	}
	if priority == configurationEvent && at == e.lastOrdinaryAt {
		panic("engine: configuration must be scheduled before ordinary work at its timestamp starts")
	}

	if e.nextID == ^EventID(0) {
		panic("engine: event ID overflow")
	}

	e.nextID++

	item := &event{id: e.nextID, at: at, priority: priority, callback: callback}
	heap.Push(&e.queue, item)

	e.byID[item.id] = item
	return item.id
}

// ScheduleAfter schedules relative to Now, or returns zero beyond the end time.
func (e *Engine) ScheduleAfter(delay time.Duration, callback func()) EventID {
	if callback == nil {
		panic("engine: callback must not be nil")
	}

	if delay < 0 {
		panic("engine: delay must not be negative")
	}

	if delay > e.endTime-e.now {
		return 0
	}
	return e.ScheduleAt(e.now+delay, callback)
}

// Cancel eagerly removes an event and releases its callback's captured state.
// It returns false for zero, unknown IDs and events that have already started.
func (e *Engine) Cancel(id EventID) bool {
	item, exists := e.byID[id]
	if !exists {
		return false
	}

	heap.Remove(&e.queue, item.index)
	delete(e.byID, id)
	item.callback = nil

	return true
}

// RunUntil executes events up to the given virtual time, inclusive.
func (e *Engine) RunUntil(until time.Duration) error {
	if e.running {
		return ErrReentrantRun
	}

	if until < e.now {
		return fmt.Errorf("engine: run bound %s is before current time %s", until, e.now)
	}

	if until > e.endTime {
		return fmt.Errorf("engine: run bound %s exceeds end time %s", until, e.endTime)
	}

	e.running = true
	defer func() {
		e.running = false
	}()

	for {
		hasEvents := len(e.queue) > 0
		if !hasEvents {
			break
		}

		nextEventIsDue := e.queue[0].at <= until
		if !nextEventIsDue {
			break
		}

		if e.executed >= e.MaxEvents {
			return fmt.Errorf("%w: %d", ErrEventLimit, e.MaxEvents)
		}

		item := heap.Pop(&e.queue).(*event)
		delete(e.byID, item.id)

		e.now = item.at
		e.executed++
		if item.priority == ordinaryEvent {
			e.lastOrdinaryAt = item.at
		}

		callback := item.callback
		item.callback = nil

		callback()
	}

	e.now = until
	return nil
}

type eventPriority uint8

const (
	configurationEvent eventPriority = iota
	ordinaryEvent
)

type event struct {
	id       EventID
	at       time.Duration
	priority eventPriority
	callback func()
	index    int
}

type eventQueue []*event

func (q eventQueue) Len() int {
	return len(q)
}

// Earlier times run first; equal times use configuration priority, then scheduling order.
func (q eventQueue) Less(i, j int) bool {
	if q[i].at == q[j].at {
		if q[i].priority != q[j].priority {
			return q[i].priority < q[j].priority
		}
		return q[i].id < q[j].id
	}
	return q[i].at < q[j].at
}

func (q eventQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index, q[j].index = i, j
}

func (q *eventQueue) Push(value any) {
	item := value.(*event)
	item.index = len(*q)
	*q = append(*q, item)
}

func (q *eventQueue) Pop() any {
	old := *q
	last := len(old) - 1
	item := old[last]
	old[last] = nil
	*q = old[:last]
	item.index = -1
	return item
}
