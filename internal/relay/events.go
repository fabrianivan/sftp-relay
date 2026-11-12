package relay

import (
	"sync"
	"time"
)

// EventKind identifies an engine event.
type EventKind string

const (
	StateChanged     EventKind = "state"
	ScanStarted      EventKind = "scan_started"
	ScanFinished     EventKind = "scan_finished"
	TransferStarted  EventKind = "transfer_started"
	TransferProgress EventKind = "transfer_progress"
	TransferDone     EventKind = "transfer_done"
	TransferFailed   EventKind = "transfer_failed"
)

// Event is published to subscribers (the GUI).
type Event struct {
	Kind        EventKind
	Time        time.Time
	Feed        string // FeedSource or FeedLocal, for transfer events
	File        string
	Size        int64
	Transferred int64
	Speed       float64 // bytes per second
	Message     string
	Err         bool
	Quiet       bool // routine scan with nothing to do (status refresh only)
}

func (e *Engine) publish(ev Event) {
	if ev.Time.IsZero() {
		ev.Time = e.now()
	}
	e.bus.publish(ev)
}

// bus fans events out without ever blocking the engine.
type bus struct {
	mu     sync.Mutex
	subs   map[int]chan Event
	next   int
	closed bool
}

func newBus() *bus { return &bus{subs: map[int]chan Event{}} }

func (b *bus) subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return ch, func() {}
	}
	id := b.next
	b.next++
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

func (b *bus) publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default: // drop for slow subscribers (progress events are frequent)
		}
	}
}

func (b *bus) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for id, ch := range b.subs {
		delete(b.subs, id)
		close(ch)
	}
}
