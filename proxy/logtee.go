package proxy

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// LogEntry is a single pre-rendered log line emitted through a LogSink.
type LogEntry struct {
	Time  time.Time
	Level slog.Level
	Line  string
}

// LogSink is a bounded channel of pre-rendered log entries. It mirrors the
// EventBus pattern (proxy/eventbus.go): non-blocking sends, atomic counters,
// and an atomic overflow flag for once-per-burst warnings.
type LogSink struct {
	ch       chan LogEntry
	emitted  atomic.Int64
	overflow atomic.Bool
}

// NewLogSink creates a LogSink with the given channel buffer capacity.
func NewLogSink(capacity int) *LogSink {
	return &LogSink{
		ch: make(chan LogEntry, capacity),
	}
}

// Subscribe returns the read-only channel that the TUI consumes from.
func (s *LogSink) Subscribe() <-chan LogEntry {
	return s.ch
}

// Snapshot drains the channel non-blockingly and returns all buffered entries.
func (s *LogSink) Snapshot() []LogEntry {
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
	return s.emitted.Load()
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
	formatH := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
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
	select {
	case h.sink.ch <- LogEntry{Time: r.Time, Level: r.Level, Line: line}:
		h.sink.overflow.Store(false)
	default:
		if !h.sink.overflow.Swap(true) {
			slog.Warn("log sink overflow, dropping entries")
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
