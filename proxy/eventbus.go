package proxy

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// RequestEvent contains metadata about a completed proxy request. It is emitted
// by EventBusMiddleware after the downstream handler finishes and is consumed
// by the TUI dashboard (or any other subscriber) via the EventBus channel.
type RequestEvent struct {
	RequestID       string
	Method          string
	Path            string
	Model           string
	Provider        string
	Status          int
	Latency         time.Duration
	MatchedProvider string
	MatchedModel    string
	Timestamp       time.Time
	ErrorMessage    string
	ErrorType       string
}

// EventBus provides a decoupled publish/subscribe channel for request metadata
// events. A single ring-buffered subscriber channel carries all events; when
// the buffer is full, Emit drops the oldest event.
type EventBus struct {
	ch       chan RequestEvent
	emitted  atomic.Int64
	mu       sync.Mutex
	overflow bool
}

// NewEventBus creates an EventBus with the given subscriber channel buffer size.
// A nil return is not used; callers that don't want an event bus simply don't
// create one — the middleware handles a nil bus pointer gracefully.
func NewEventBus(bufferSize int) *EventBus {
	return &EventBus{
		ch: make(chan RequestEvent, bufferSize),
	}
}

// Emit sends an event to the subscriber channel without blocking. If the
// channel is full, the event is dropped and a warning is logged once per
// overflow burst.
func (b *EventBus) Emit(e RequestEvent) {
	if b == nil {
		return
	}
	e.Timestamp = time.Now()
	b.emitted.Add(1)
	select {
	case b.ch <- e:
		b.mu.Lock()
		b.overflow = false
		b.mu.Unlock()
	default:
		b.mu.Lock()
		if !b.overflow {
			b.overflow = true
			slog.Warn("event bus overflow, dropping events")
		}
		b.mu.Unlock()
	}
}

// Subscribe returns the read-only subscriber channel. All subscribers share
// the same channel; events are delivered to whichever subscriber reads first.
func (b *EventBus) Subscribe() <-chan RequestEvent {
	if b == nil {
		return nil
	}
	return b.ch
}

// EventCount returns the total number of events emitted since the bus was
// created (including dropped events).
func (b *EventBus) EventCount() int {
	if b == nil {
		return 0
	}
	return int(b.emitted.Load())
}
