package proxy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// LogEntry is a single pre-rendered log line emitted through a LogSink.
type LogEntry struct {
	Seq   int64
	Time  time.Time
	Level slog.Level
	Line  string
}

// LogSink is a bounded channel of pre-rendered log entries. It mirrors the
// EventBus pattern (proxy/eventbus.go): non-blocking sends, atomic counters,
// and an atomic overflow flag for once-per-burst warnings. A ring buffer stores
// recent entries for IPC replay.
type LogSink struct {
	emitted  atomic.Int64
	overflow atomic.Bool
	mu       sync.Mutex
	subs     map[int]chan LogEntry
	subID    int

	ring    []LogEntry
	ringMu  sync.RWMutex
	ringCap int
	head    int // Index of oldest entry.
	ringLen int // Number of valid entries.
	seq     atomic.Int64
}

// NewLogSink creates a LogSink with the given channel buffer capacity.
func NewLogSink(_ int) *LogSink {
	return &LogSink{
		subs:    make(map[int]chan LogEntry),
		ring:    make([]LogEntry, 0, 10000),
		ringCap: 10000,
	}
}

// Subscribe returns a new read-only channel for this subscriber.
func (s *LogSink) Subscribe() <-chan LogEntry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan LogEntry, 100) // Per-subscriber buffer
	s.subID++
	id := s.subID
	s.subs[id] = ch

	return ch
}

// Unsubscribe removes a subscriber channel from the sink.
func (s *LogSink) Unsubscribe(ch <-chan LogEntry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, sub := range s.subs {
		if sub == ch {
			delete(s.subs, id)
			close(sub)
			return
		}
	}
}

// Snapshot returns a copy of the ring buffer for IPC replay.
func (s *LogSink) Snapshot() []LogEntry {
	if s == nil {
		return nil
	}
	s.ringMu.RLock()
	defer s.ringMu.RUnlock()

	out := make([]LogEntry, s.ringLen)
	for i := 0; i < s.ringLen; i++ {
		out[i] = s.ring[(s.head+i)%s.ringCap]
	}
	return out
}

// EventCount returns the total number of log entries emitted (including drops).
func (s *LogSink) EventCount() int64 {
	if s == nil {
		return 0
	}
	return s.emitted.Load()
}

// SnapshotSince returns buffered log entries with Seq >= seq for IPC replay.
// This is non-destructive (reads from ring buffer copy, not channel).
// Returns (entries, currentSeq, evicted).
//   - seq <= 0: return entire ring, evicted=false.
//   - seq > currentSeq: return nil, currentSeq, false.
//   - seq == currentSeq: return nil, currentSeq, false.
//   - seq < oldest_in_ring: return what's left, evicted=true.
func (s *LogSink) SnapshotSince(seq int64) ([]LogEntry, int64, bool) {
	if s == nil {
		return nil, 0, false
	}

	currentSeq := s.seq.Load()

	if seq > currentSeq {
		return nil, currentSeq, false
	}
	if seq == currentSeq {
		return nil, currentSeq, false
	}

	s.ringMu.RLock()
	defer s.ringMu.RUnlock()

	if s.ringLen == 0 {
		return nil, currentSeq, false
	}

	if seq <= 0 {
		out := make([]LogEntry, s.ringLen)
		for i := 0; i < s.ringLen; i++ {
			out[i] = s.ring[(s.head+i)%s.ringCap]
		}
		return out, currentSeq, false
	}

	evicted := false
	var out []LogEntry
	for i := 0; i < s.ringLen; i++ {
		e := s.ring[(s.head+i)%s.ringCap]
		if e.Seq < seq {
			continue
		}
		if len(out) == 0 && e.Seq > seq {
			evicted = true
		}
		out = append(out, e)
	}

	if len(out) == 0 {
		return nil, currentSeq, false
	}

	result := make([]LogEntry, len(out))
	copy(result, out)
	return result, currentSeq, evicted
}

// ringHandler is a slog.Handler that fans every record out to (a) the wrapped
// stderr handler and (b) a text-format child handler writing into a shared
// buffer whose result is pushed into the LogSink channel.
type ringHandler struct {
	inner     slog.Handler
	formatH   slog.Handler
	formatBuf *bytes.Buffer
	mu        *sync.Mutex
	sink      *LogSink
}

// NewRingHandler creates a slog.Handler that fans out to the inner handler
// (stderr) and pushes pre-rendered text copies into sink.
func NewRingHandler(inner slog.Handler, sink *LogSink) slog.Handler {
	buf := new(bytes.Buffer)
	formatH := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return &ringHandler{
		inner:     inner,
		formatH:   formatH,
		formatBuf: buf,
		mu:        &sync.Mutex{},
		sink:      sink,
	}
}

// Enabled implements slog.Handler. The tee never drops at the handler boundary
// because level filtering is a TUI renderer concern, not a logging concern.
func (h *ringHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

// Handle implements slog.Handler. It clones the record, pre-renders it to
// text via formatH, non-blockingly pushes the result into the sink channel,
// and also delegates to the inner handler (stderr) if the inner handler
// would accept the record. The tee's own Enabled is always permissive so the
// TUI sees all levels; stderr preserves its native level filter.
func (h *ringHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.inner.Enabled(ctx, r.Level) {
		if err := h.inner.Handle(ctx, r); err != nil {
			return err
		}
	}

	h.mu.Lock()
	h.formatBuf.Reset()
	err := h.formatH.Handle(ctx, r.Clone())
	line := h.formatBuf.String()
	h.mu.Unlock()

	if err != nil {
		return err
	}

	h.sink.emitted.Add(1)
	entry := LogEntry{
		Seq:   h.sink.seq.Add(1),
		Time:  r.Time,
		Level: r.Level,
		Line:  line,
	}

	// Store in ring buffer for IPC replay (circular buffer).
	h.sink.ringMu.Lock()
	idx := (h.sink.head + h.sink.ringLen) % h.sink.ringCap
	if h.sink.ringLen < h.sink.ringCap {
		h.sink.ring = append(h.sink.ring, entry)
		h.sink.ringLen++
	} else {
		h.sink.ring[idx] = entry
		h.sink.head = (h.sink.head + 1) % h.sink.ringCap
	}
	h.sink.ringMu.Unlock()

	// Broadcast to all subscribers.
	h.sink.mu.Lock()
	for id, ch := range h.sink.subs {
		select {
		case ch <- entry:
			// Event delivered.
		default:
			// Channel full; drop for this subscriber.
			if !h.sink.overflow.Swap(true) {
				fmt.Fprintln(os.Stderr, "log sink subscriber overflow", "subscriber", id)
			}
		}
	}
	h.sink.mu.Unlock()

	return nil
}

// WithAttrs implements slog.Handler. It returns a new ringHandler wrapping
// both inner and formatH handlers with the additional attributes. The shared
// format buffer and mutex are reused across clones so that WithAttrs/WithGroup
// chains remain synchronized on the same writer.
func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ringHandler{
		inner:     h.inner.WithAttrs(attrs),
		formatH:   h.formatH.WithAttrs(attrs),
		formatBuf: h.formatBuf,
		mu:        h.mu,
		sink:      h.sink,
	}
}

// WithGroup implements slog.Handler.
func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{
		inner:     h.inner.WithGroup(name),
		formatH:   h.formatH.WithGroup(name),
		formatBuf: h.formatBuf,
		mu:        h.mu,
		sink:      h.sink,
	}
}
