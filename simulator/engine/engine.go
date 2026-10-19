package engine

import (
	"container/heap"
	"errors"
	"fmt"
	"time"
)

// EventID identifies one scheduled callback
type EventID uint64

const DefaultMaxEvents uint64 = 50_000_000

var (
	ErrEventLimit   = errors.New("engine: event execution limit reached")
	ErrReentrantRun = errors.New("engine: RunUntil cannot be called from a running callback")
)

// Engine owns a global future-event queue. equal-time events run in increasing
// EventID order, including events scheduled by another callback at the same time.
type Engine struct {
	// MaxEvents bounds the total number of callbacks executed over all RunUntil
	MaxEvents uint64
	now       time.Duration
	endTime   time.Duration // virtual end time; no events are scheduled after it.
	nextID    EventID
	queue     eventQueue
	byID      map[EventID]*event
	executed  uint64
	running   bool
}

// New creates a simulation engine with its clock starting at zero.
func New(endTime time.Duration) *Engine {
	if endTime < 0 {
		panic("engine: end time must not be negative")
	}
	return &Engine{endTime: endTime, MaxEvents: DefaultMaxEvents, byID: make(map[EventID]*event)}
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

func (e *Engine) EventsExecuted() uint64 {
	return e.executed
}

// ScheduleAt schedules a callback, or returns zero when at exceeds EndTime.
func (e *Engine) ScheduleAt(at time.Duration, callback func()) EventID {
	if callback == nil {
		panic("engine: callback must not be nil")
	}

	if at < 0 || at < e.now {
		panic("engine: cannot schedule an event in the past")
	}

	if at > e.endTime {
		return 0
	}

	if e.nextID == ^EventID(0) {
		panic("engine: event ID overflow")
	}

	e.nextID++

	item := &event{id: e.nextID, at: at, callback: callback}
	heap.Push(&e.queue, item)

	e.byID[item.id] = item
	return item.id
}

// ScheduleAfter schedules relative to Now. Comparing with the remaining time
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

		callback := item.callback
		item.callback = nil

		callback()
	}

	e.now = until
	return nil
}

type event struct {
	id       EventID
	at       time.Duration
	callback func()
	index    int
}

type eventQueue []*event

func (q eventQueue) Len() int {
	return len(q)
}

// less puts earlier events first, equal-time events follow scheduling order (ID).
func (q eventQueue) Less(i, j int) bool {
	if q[i].at == q[j].at {
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
