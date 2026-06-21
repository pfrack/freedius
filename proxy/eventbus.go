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
	Seq             int64
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
// the buffer is full, Emit drops the oldest event. A ring buffer stores recent
// events for replay on late IPC attach.
type EventBus struct {
	ch       chan RequestEvent
	emitted  atomic.Int64
	mu       sync.Mutex
	overflow bool

	ring    []RequestEvent
	ringMu  sync.RWMutex
	ringCap int
	head    int // Index of oldest entry.
	ringLen int // Number of valid entries.
	seq     atomic.Int64
}

// NewEventBus creates an EventBus with the given subscriber channel buffer size.
// A nil return is not used; callers that don't want an event bus simply don't
// create one — the middleware handles a nil bus pointer gracefully.
func NewEventBus(bufferSize int) *EventBus {
	return &EventBus{
		ch:      make(chan RequestEvent, bufferSize),
		ring:    make([]RequestEvent, 0, 10000),
		ringCap: 10000,
	}
}

// Emit sends an event to the subscriber channel without blocking. If the
// channel is full, the event is dropped and a warning is logged once per
// overflow burst. The event is also stored in the ring buffer for IPC replay.
func (b *EventBus) Emit(e RequestEvent) {
	if b == nil {
		return
	}
	e.Timestamp = time.Now()
	e.Seq = b.seq.Add(1)
	b.emitted.Add(1)

	// Store in ring buffer for IPC replay (circular buffer).
	b.ringMu.Lock()
	idx := (b.head + b.ringLen) % b.ringCap
	if b.ringLen < b.ringCap {
		b.ring = append(b.ring, e)
		b.ringLen++
	} else {
		b.ring[idx] = e
		b.head = (b.head + 1) % b.ringCap
	}
	b.ringMu.Unlock()

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

// Since returns buffered events with Seq >= seq for IPC replay.
// Returns (events, currentSeq, evicted).
//   - seq <= 0: return entire ring, evicted=false.
//   - seq > currentSeq: return nil, currentSeq, false (nothing to replay).
//   - seq == currentSeq: return nil, currentSeq, false (caught up).
//   - seq < oldest_in_ring: return what's left, evicted=true.
func (b *EventBus) Since(seq int64) ([]RequestEvent, int64, bool) {
	if b == nil {
		return nil, 0, false
	}

	currentSeq := b.seq.Load()

	if seq > currentSeq {
		return nil, currentSeq, false
	}
	if seq == currentSeq {
		return nil, currentSeq, false
	}

	b.ringMu.RLock()
	defer b.ringMu.RUnlock()

	if b.ringLen == 0 {
		return nil, currentSeq, false
	}

	// seq <= 0 means initial attach: return entire ring.
	if seq <= 0 {
		out := make([]RequestEvent, b.ringLen)
		for i := 0; i < b.ringLen; i++ {
			out[i] = b.ring[(b.head+i)%b.ringCap]
		}
		return out, currentSeq, false
	}

	// Find events with Seq >= seq.
	evicted := false
	var out []RequestEvent
	for i := 0; i < b.ringLen; i++ {
		e := b.ring[(b.head+i)%b.ringCap]
		if e.Seq < seq {
			continue
		}
		if len(out) == 0 && e.Seq > seq {
			// The first event we'd return has Seq > seq, meaning
			// the requested seq was evicted from the ring.
			evicted = true
		}
		out = append(out, e)
	}

	if len(out) == 0 {
		return nil, currentSeq, false
	}

	result := make([]RequestEvent, len(out))
	copy(result, out)
	return result, currentSeq, evicted
}
