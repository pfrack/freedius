package proxy

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// disabledHandler is a slog.Handler whose Enabled always returns false.
type disabledHandler struct {
	slog.Handler
}

func (disabledHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return false
}

func (disabledHandler) Handle(_ context.Context, _ slog.Record) error {
	return nil
}

func (disabledHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return disabledHandler{}
}

func (disabledHandler) WithGroup(_ string) slog.Handler {
	return disabledHandler{}
}

func TestRingHandler_EmitsToStderrAndBuffer(t *testing.T) {
	var stderrBuf bytes.Buffer
	inner := slog.NewTextHandler(&stderrBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	sink := NewLogSink(10)
	handler := NewRingHandler(inner, sink)
	logger := slog.New(handler)

	logger.Info("hello world")

	if stderrBuf.Len() == 0 {
		t.Error("expected stderr output")
	}
	entries := sink.Snapshot()
	if len(entries) == 0 {
		t.Fatal("expected log entries in sink")
	}
	if !strings.Contains(entries[0].Line, "hello world") {
		t.Errorf("entry Line = %q, want contains 'hello world'", entries[0].Line)
	}
	if entries[0].Level != slog.LevelInfo {
		t.Errorf("entry Level = %v, want LevelInfo", entries[0].Level)
	}
}

func TestRingHandler_PreRenderIsTextShapeRegardlessOfStderrFormat(t *testing.T) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	jsonSink := NewLogSink(10)
	var jsonStderr bytes.Buffer
	jsonInner := slog.NewJSONHandler(&jsonStderr, opts)
	jsonHandler := NewRingHandler(jsonInner, jsonSink)
	jsonLogger := slog.New(jsonHandler)

	textSink := NewLogSink(10)
	var textStderr bytes.Buffer
	textInner := slog.NewTextHandler(&textStderr, opts)
	textHandler := NewRingHandler(textInner, textSink)
	textLogger := slog.New(textHandler)

	jsonLogger.Info("test msg")
	textLogger.Info("test msg")

	jsonEntries := jsonSink.Snapshot()
	textEntries := textSink.Snapshot()

	if len(jsonEntries) == 0 {
		t.Fatal("expected entries from json stderr logger")
	}
	if len(textEntries) == 0 {
		t.Fatal("expected entries from text stderr logger")
	}

	stripTime := func(line string) string {
		if idx := strings.Index(line, " level="); idx >= 0 {
			return line[idx+1:]
		}
		return line
	}
	jsonNorm := stripTime(jsonEntries[0].Line)
	textNorm := stripTime(textEntries[0].Line)
	if jsonNorm != textNorm {
		t.Errorf("pre-rendered lines (time-stripped) differ: json=%q text=%q", jsonNorm, textNorm)
	}
}

func TestRingHandler_DropsOnOverflow(t *testing.T) {
	capacity := 5
	sink := NewLogSink(capacity)
	handler := NewRingHandler(disabledHandler{}, sink)
	logger := slog.New(handler)

	// Subscribe before emitting so we receive events.
	ch := sink.Subscribe()

	total := capacity + 10
	for i := 0; i < total; i++ {
		logger.Info("test")
	}

	// Drain subscriber channel — with a 100-element per-subscriber buffer,
	// all events should arrive (fan-out is non-blocking with per-subscriber buffers).
	read := 0
	for {
		select {
		case <-ch:
			read++
		case <-time.After(100 * time.Millisecond):
			goto done
		}
	}
done:
	// The subscriber channel has a 100-element buffer, so all 15 events
	// should arrive. The old test checked that only `capacity` arrived,
	// but with fan-out each subscriber gets its own buffered channel.
	if read != total {
		t.Errorf("subscriber read %d events, expected %d", read, total)
	}
	if got := sink.EventCount(); got != int64(total) {
		t.Errorf("EventCount = %d, want %d", got, total)
	}
}

func TestRingHandler_Concurrent(t *testing.T) {
	sink := NewLogSink(100)
	handler := NewRingHandler(disabledHandler{}, sink)
	logger := slog.New(handler)

	var wg sync.WaitGroup
	records := 1000
	workers := 8

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range records {
				logger.Info("concurrent test", "n", 1)
			}
		}()
	}

	wg.Wait()

	entries := sink.Snapshot()
	dropped := int(sink.EventCount()) - len(entries)
	if int(sink.EventCount()) != workers*records {
		t.Errorf("EventCount = %d, want %d", sink.EventCount(), workers*records)
	}
	if len(entries)+dropped != workers*records {
		t.Errorf("entries(%d) + dropped(%d) = %d, want %d",
			len(entries), dropped, len(entries)+dropped, workers*records)
	}
}

func TestRingHandler_Enabled(t *testing.T) {
	sink := NewLogSink(10)
	handler := NewRingHandler(disabledHandler{}, sink)
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("ringHandler.Enabled should return true even when inner returns false")
	}
	if !handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("ringHandler.Enabled should return true for all levels")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("ringHandler.Enabled should return true for all levels")
	}
}
