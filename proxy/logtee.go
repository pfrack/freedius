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
	ch       chan LogEntry
	emitted  atomic.Int64
	overflow atomic.Bool

	ring    []LogEntry
	ringMu  sync.RWMutex
	ringCap int
	seq     atomic.Int64
}

// NewLogSink creates a LogSink with the given channel buffer capacity.
func NewLogSink(capacity int) *LogSink {
	return &LogSink{
		ch:      make(chan LogEntry, capacity),
		ring:    make([]LogEntry, 0, 10000),
		ringCap: 10000,
	}
}

// Subscribe returns the read-only channel that the TUI consumes from.
func (s *LogSink) Subscribe() <-chan LogEntry {
	if s == nil {
		return nil
	}
	return s.ch
}

// Snapshot drains the channel non-blockingly and returns all buffered entries.
func (s *LogSink) Snapshot() []LogEntry {
	if s == nil {
		return nil
	}
	out := make([]LogEntry, 0, cap(s.ch))
	for {
		select {
		case e := <-s.ch:
			out = append(out, e)
		default:
			return out
		}
	}
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

	if len(s.ring) == 0 {
		return nil, currentSeq, false
	}

	if seq <= 0 {
		out := make([]LogEntry, len(s.ring))
		copy(out, s.ring)
		return out, currentSeq, false
	}

	evicted := false
	var out []LogEntry
	for _, e := range s.ring {
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

	// Store in ring buffer for IPC replay.
	h.sink.ringMu.Lock()
	if len(h.sink.ring) >= h.sink.ringCap {
		copy(h.sink.ring, h.sink.ring[1:])
		h.sink.ring[len(h.sink.ring)-1] = entry
	} else {
		h.sink.ring = append(h.sink.ring, entry)
	}
	h.sink.ringMu.Unlock()

	select {
	case h.sink.ch <- entry:
		h.sink.overflow.Store(false)
	default:
		if !h.sink.overflow.Swap(true) {
			fmt.Fprintln(os.Stderr, "log sink overflow, dropping entries")
		}
	}

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
