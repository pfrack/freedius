package proxy

import (
	"sync"
	"testing"
	"time"
)

func TestStatsCollector_ZeroState(t *testing.T) {
	sc := NewStatsCollector(nil)
	ms := sc.MappingSnapshot()
	ps := sc.ProviderSnapshot()
	if len(ms) != 0 {
		t.Fatalf("expected empty mapping snapshot, got %d entries", len(ms))
	}
	if len(ps) != 0 {
		t.Fatalf("expected empty provider snapshot, got %d entries", len(ps))
	}
}

func TestStatsCollector_NilReceiver(t *testing.T) {
	var sc *StatsCollector
	// Should not panic.
	ms := sc.MappingSnapshot()
	ps := sc.ProviderSnapshot()
	sc.RecordFallback("test")
	if ms != nil {
		t.Fatalf("expected nil mapping snapshot from nil receiver")
	}
	if ps != nil {
		t.Fatalf("expected nil provider snapshot from nil receiver")
	}
}

func TestStatsCollector_SingleEvent(t *testing.T) {
	bus := NewEventBus(100)
	sc := NewStatsCollector(bus)

	bus.Emit(RequestEvent{
		Model:           "haiku",
		MatchedProvider: "nim",
		MatchedModel:    "stepfun-ai/step-3.5-flash",
		Status:          200,
		Latency:         150 * time.Millisecond,
	})

	// Give the consumer goroutine time to process.
	time.Sleep(20 * time.Millisecond)

	ms := sc.MappingSnapshot()
	if ms["haiku"].RequestCount != 1 {
		t.Errorf("expected request count 1, got %d", ms["haiku"].RequestCount)
	}
	if ms["haiku"].ErrorCount != 0 {
		t.Errorf("expected error count 0, got %d", ms["haiku"].ErrorCount)
	}
	if ms["haiku"].LastLatency != 150*time.Millisecond {
		t.Errorf("expected latency 150ms, got %v", ms["haiku"].LastLatency)
	}
	if ms["haiku"].RecentErrorRate != 0 {
		t.Errorf("expected 0 error rate, got %f", ms["haiku"].RecentErrorRate)
	}

	ps := sc.ProviderSnapshot()
	if ps["nim"].RequestCount != 1 {
		t.Errorf("expected provider request count 1, got %d", ps["nim"].RequestCount)
	}
	if ps["nim"].LastSuccess.IsZero() {
		t.Error("expected non-zero LastSuccess")
	}
}

func TestStatsCollector_ErrorEvent(t *testing.T) {
	bus := NewEventBus(100)
	sc := NewStatsCollector(bus)

	bus.Emit(RequestEvent{
		Model:           "opus",
		MatchedProvider: "go",
		Status:          500,
		Latency:         200 * time.Millisecond,
		ErrorType:       "upstream_error",
		ErrorMessage:    "internal server error",
	})

	time.Sleep(20 * time.Millisecond)

	ms := sc.MappingSnapshot()
	if ms["opus"].ErrorCount != 1 {
		t.Errorf("expected error count 1, got %d", ms["opus"].ErrorCount)
	}
	if ms["opus"].RecentErrorRate != 1.0 {
		t.Errorf("expected 1.0 error rate, got %f", ms["opus"].RecentErrorRate)
	}

	ps := sc.ProviderSnapshot()
	if ps["go"].ErrorCount != 1 {
		t.Errorf("expected provider error count 1, got %d", ps["go"].ErrorCount)
	}
	if ps["go"].LastErrorMessage != "internal server error" {
		t.Errorf("expected error message, got %q", ps["go"].LastErrorMessage)
	}
	if ps["go"].LastError.IsZero() {
		t.Error("expected non-zero LastError")
	}
}

func TestStatsCollector_MultipleEvents(t *testing.T) {
	bus := NewEventBus(100)
	sc := NewStatsCollector(bus)

	for i := 0; i < 5; i++ {
		status := 200
		if i%2 == 0 {
			status = 500
		}
		bus.Emit(RequestEvent{
			Model:           "sonnet",
			MatchedProvider: "nim",
			Status:          status,
			Latency:         time.Duration(i+1) * 100 * time.Millisecond,
		})
	}

	time.Sleep(50 * time.Millisecond)

	ms := sc.MappingSnapshot()
	if ms["sonnet"].RequestCount != 5 {
		t.Errorf("expected 5 requests, got %d", ms["sonnet"].RequestCount)
	}
	// Events 0, 2, 4 are errors (status 500).
	if ms["sonnet"].ErrorCount != 3 {
		t.Errorf("expected 3 errors, got %d", ms["sonnet"].ErrorCount)
	}
	// Recent error rate: 3/5 = 0.6.
	expectedRate := 0.6
	if ms["sonnet"].RecentErrorRate != expectedRate {
		t.Errorf("expected error rate %f, got %f", expectedRate, ms["sonnet"].RecentErrorRate)
	}
}

func TestStatsCollector_RecentWindowRollover(t *testing.T) {
	bus := NewEventBus(100)
	sc := NewStatsCollector(bus)

	// Emit 10 errors, then 10 successes. The recent window (10) should
	// only reflect the last 10 (all successes).
	for i := 0; i < 10; i++ {
		bus.Emit(RequestEvent{
			Model:           "auto",
			MatchedProvider: "go",
			Status:          500,
		})
	}
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < 10; i++ {
		bus.Emit(RequestEvent{
			Model:           "auto",
			MatchedProvider: "go",
			Status:          200,
		})
	}
	time.Sleep(20 * time.Millisecond)

	ms := sc.MappingSnapshot()
	if ms["auto"].RequestCount != 20 {
		t.Errorf("expected 20 requests, got %d", ms["auto"].RequestCount)
	}
	if ms["auto"].ErrorCount != 10 {
		t.Errorf("expected 10 total errors, got %d", ms["auto"].ErrorCount)
	}
	// Recent window should be 0.0 (last 10 are all success).
	if ms["auto"].RecentErrorRate != 0.0 {
		t.Errorf("expected 0.0 recent error rate, got %f", ms["auto"].RecentErrorRate)
	}
}

func TestStatsCollector_FallbackCount(t *testing.T) {
	sc := NewStatsCollector(nil)

	sc.RecordFallback("haiku")
	sc.RecordFallback("haiku")
	sc.RecordFallback("sonnet")

	ms := sc.MappingSnapshot()
	if ms["haiku"].FallbackCount != 2 {
		t.Errorf("expected 2 fallbacks for haiku, got %d", ms["haiku"].FallbackCount)
	}
	if ms["sonnet"].FallbackCount != 1 {
		t.Errorf("expected 1 fallback for sonnet, got %d", ms["sonnet"].FallbackCount)
	}
}

func TestStatsCollector_SkipsEmptyModel(t *testing.T) {
	bus := NewEventBus(100)
	sc := NewStatsCollector(bus)

	// Events with empty Model (e.g., health check hits) should be ignored.
	bus.Emit(RequestEvent{
		Model:  "",
		Status: 200,
	})
	time.Sleep(20 * time.Millisecond)

	ms := sc.MappingSnapshot()
	if len(ms) != 0 {
		t.Errorf("expected empty mapping snapshot, got %d entries", len(ms))
	}
}

func TestStatsCollector_ConcurrentAccess(t *testing.T) {
	bus := NewEventBus(100)
	sc := NewStatsCollector(bus)

	var wg sync.WaitGroup
	// Writer: emit events.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			bus.Emit(RequestEvent{
				Model:           "concurrent",
				MatchedProvider: "nim",
				Status:          200,
				Latency:         time.Millisecond,
			})
		}
	}()

	// Reader: take snapshots concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = sc.MappingSnapshot()
			_ = sc.ProviderSnapshot()
		}
	}()

	// Fallback recorder: concurrent writes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			sc.RecordFallback("concurrent")
		}
	}()

	wg.Wait()
	time.Sleep(30 * time.Millisecond)

	ms := sc.MappingSnapshot()
	if ms["concurrent"].RequestCount < 1 {
		t.Error("expected at least 1 request recorded")
	}
}

func TestStatsCollector_MappingNameResolution(t *testing.T) {
	bus := NewEventBus(100)
	sc := NewStatsCollector(bus)

	// A request for "claude-sonnet-4-20250514" resolves to mapping "sonnet"
	// by family. The collector must attribute to the resolved mapping name,
	// not the raw requested model.
	bus.Emit(RequestEvent{
		Model:           "claude-sonnet-4-20250514",
		MappingName:     "sonnet",
		MatchedProvider: "nim",
		Status:          200,
	})
	time.Sleep(20 * time.Millisecond)

	ms := sc.MappingSnapshot()
	if _, ok := ms["sonnet"]; !ok {
		t.Fatalf("expected stats keyed under resolved mapping 'sonnet', got keys %v", keysOf(ms))
	}
	if _, ok := ms["claude-sonnet-4-20250514"]; ok {
		t.Errorf("stats should NOT be keyed under the raw requested model")
	}
	if ms["sonnet"].RequestCount != 1 {
		t.Errorf("expected 1 request under 'sonnet', got %d", ms["sonnet"].RequestCount)
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestStatsCollector_MultipleProviders(t *testing.T) {
	bus := NewEventBus(100)
	sc := NewStatsCollector(bus)

	bus.Emit(RequestEvent{
		Model:           "haiku",
		MatchedProvider: "nim",
		Status:          200,
	})
	bus.Emit(RequestEvent{
		Model:           "haiku",
		MatchedProvider: "go",
		Status:          500,
		ErrorMessage:    "timeout",
	})
	time.Sleep(20 * time.Millisecond)

	ps := sc.ProviderSnapshot()
	if ps["nim"].RequestCount != 1 {
		t.Errorf("expected nim request count 1, got %d", ps["nim"].RequestCount)
	}
	if ps["go"].RequestCount != 1 {
		t.Errorf("expected go request count 1, got %d", ps["go"].RequestCount)
	}
	if ps["go"].ErrorCount != 1 {
		t.Errorf("expected go error count 1, got %d", ps["go"].ErrorCount)
	}
}
