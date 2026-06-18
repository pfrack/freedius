package proxy

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus_EmitAndSubscribe(t *testing.T) {
	bus := NewEventBus(10)

	events := []RequestEvent{
		{RequestID: "a", Model: "opus", Provider: "nim", Status: 200, Latency: 10 * time.Millisecond},
		{RequestID: "b", Model: "sonnet", Provider: "anthropic", Status: 200, Latency: 50 * time.Millisecond},
		{RequestID: "c", Model: "haiku", Provider: "openai", Status: 500, Latency: 100 * time.Millisecond},
	}

	for i := range events {
		bus.Emit(events[i])
	}

	ch := bus.Subscribe()
	for i, expected := range events {
		select {
		case got := <-ch:
			if got.RequestID != expected.RequestID {
				t.Errorf("event %d: RequestID = %q, want %q", i, got.RequestID, expected.RequestID)
			}
			if got.Model != expected.Model {
				t.Errorf("event %d: Model = %q, want %q", i, got.Model, expected.Model)
			}
			if got.Provider != expected.Provider {
				t.Errorf("event %d: Provider = %q, want %q", i, got.Provider, expected.Provider)
			}
			if got.Status != expected.Status {
				t.Errorf("event %d: Status = %d, want %d", i, got.Status, expected.Status)
			}
			if got.Timestamp.IsZero() {
				t.Errorf("event %d: Timestamp is zero (not set by Emit)", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("event %d: timed out waiting for event", i)
		}
	}

	if got := bus.EventCount(); got != 3 {
		t.Errorf("EventCount = %d, want 3", got)
	}
}

func TestEventBus_Overflow(t *testing.T) {
	bufSize := 2
	bus := NewEventBus(bufSize)

	// Fill the buffer.
	for i := 0; i < bufSize; i++ {
		bus.Emit(RequestEvent{RequestID: "fill", Status: 200})
	}

	// Overflow: emit more events than the buffer can hold.
	for i := 0; i < 5; i++ {
		bus.Emit(RequestEvent{RequestID: "overflow", Status: 500})
	}

	// After overflow, the oldest events should be dropped.
	// We should be able to read bufSize events from the channel.
	ch := bus.Subscribe()
	read := 0
timeout:
	for i := 0; i < bufSize; i++ {
		select {
		case <-ch:
			read++
		case <-time.After(100 * time.Millisecond):
			break timeout
		}
	}
	if read != bufSize {
		t.Errorf("read %d events after overflow, want %d", read, bufSize)
	}

	// EventCount counts all emitted events (including dropped).
	if got := bus.EventCount(); got != 7 {
		t.Errorf("EventCount = %d, want 7", got)
	}
}

func TestEventBus_Concurrent(t *testing.T) {
	bus := NewEventBus(1000)
	var wg sync.WaitGroup
	const goroutines = 10
	const eventsPerGoroutine = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				bus.Emit(RequestEvent{
					RequestID: "concurrent",
					Status:    200,
				})
			}
		}(g)
	}
	wg.Wait()

	expected := goroutines * eventsPerGoroutine
	if got := bus.EventCount(); got != expected {
		t.Errorf("EventCount = %d, want %d", got, expected)
	}

	// Drain the channel (up to buffer size).
	ch := bus.Subscribe()
	drained := 0
	for drained < 1000 {
		select {
		case <-ch:
			drained++
		default:
			goto drainDone
		}
	}
drainDone:
	if drained < 1 {
		t.Error("expected at least one event in channel after concurrent emits")
	}
}

func TestEventBus_NilBus(t *testing.T) {
	var bus *EventBus

	// All methods on nil receiver must not panic.
	bus.Emit(RequestEvent{RequestID: "test"})
	if ch := bus.Subscribe(); ch != nil {
		t.Error("Subscribe on nil bus should return nil channel")
	}
	if got := bus.EventCount(); got != 0 {
		t.Errorf("EventCount on nil bus = %d, want 0", got)
	}
}
