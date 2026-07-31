package proxy

import (
	"sync"
	"time"
)

// recentWindow is the number of recent events tracked for error rate
// calculation per mapping/provider.
const recentWindow = 10

// MappingStats holds aggregate telemetry for a single mapping.
type MappingStats struct {
	RequestCount    int64
	ErrorCount      int64
	FallbackCount   int64
	LastActivity    time.Time
	LastLatency     time.Duration
	RecentErrorRate float64 // Error rate over last recentWindow requests (0.0–1.0).
}

// ProviderStats holds aggregate telemetry for a single provider.
type ProviderStats struct {
	RequestCount     int64
	ErrorCount       int64
	LastSuccess      time.Time
	LastError        time.Time
	LastErrorMessage string
	RecentErrorRate  float64 // Error rate over last recentWindow requests (0.0–1.0).
}

// StatsCollector subscribes to an EventBus and maintains per-mapping and
// per-provider aggregate counters. The web dashboard reads snapshots on render.
//
// Thread-safety: all fields are guarded by mu (sync.RWMutex). Follows the
// sync.Mutex+map pattern per lessons.md §7 — Snapshot/iteration is a primary
// operation.
type StatsCollector struct {
	mu        sync.RWMutex
	mappings  map[string]*mappingAccum
	providers map[string]*providerAccum
}

// mappingAccum is the internal accumulator for a mapping's stats.
type mappingAccum struct {
	requestCount  int64
	errorCount    int64
	fallbackCount int64
	lastActivity  time.Time
	lastLatency   time.Duration
	recentErrors  []bool // circular buffer of last N outcomes (true = error)
	recentIdx     int
	recentLen     int
}

// providerAccum is the internal accumulator for a provider's stats.
type providerAccum struct {
	requestCount     int64
	errorCount       int64
	lastSuccess      time.Time
	lastError        time.Time
	lastErrorMessage string
	recentErrors     []bool // circular buffer of last N outcomes (true = error)
	recentIdx        int
	recentLen        int
}

// NewStatsCollector creates a StatsCollector and starts a background goroutine
// that subscribes to the EventBus. When bus is nil, the collector is inert
// (still usable — snapshots return empty maps).
func NewStatsCollector(bus *EventBus) *StatsCollector {
	sc := &StatsCollector{
		mappings:  make(map[string]*mappingAccum),
		providers: make(map[string]*providerAccum),
	}
	if bus != nil {
		ch := bus.Subscribe()
		go sc.consume(ch)
	}
	return sc
}

// consume drains the subscriber channel and updates internal state.
func (sc *StatsCollector) consume(ch <-chan RequestEvent) {
	for ev := range ch {
		sc.record(ev)
	}
}

// record processes a single RequestEvent and updates counters.
func (sc *StatsCollector) record(ev RequestEvent) {
	// Determine the mapping name from the Model field (the freedius-facing
	// name sent in the request body). Skip non-routing events (health checks
	// and events with no model).
	mappingName := ev.Model
	if mappingName == "" {
		return
	}
	providerName := ev.MatchedProvider
	if providerName == "" {
		providerName = ev.Provider
	}

	isError := ev.Status >= 400
	// Detect fallback: if the original request model resolves to a mapping
	// whose primary provider differs from the matched provider, it's a
	// fallback event. However, we don't have access to config here — instead
	// we use the ErrorType heuristic: if the error mentions "all providers
	// failed" or the event has specific fallback indicators.
	// Simpler approach: the LastResponder records fallback usage; here we
	// detect fallback by checking if ErrorType contains "fallback" in the
	// aggregated error, or by trusting that the caller will enrich.
	//
	// Actually, the most reliable signal: if the event has a non-empty
	// ErrorType that is NOT a client error (4xx from the proxy itself),
	// and the MatchedProvider is present, we count it as routed to that
	// provider. For fallback detection, we rely on the proxy logging
	// "fallback succeeded" which means the response came from a non-primary.
	// Since we can't distinguish primary vs fallback from RequestEvent alone,
	// we'll track fallback separately via a dedicated method.

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Update mapping stats.
	ma := sc.mappings[mappingName]
	if ma == nil {
		ma = &mappingAccum{
			recentErrors: make([]bool, recentWindow),
		}
		sc.mappings[mappingName] = ma
	}
	ma.requestCount++
	if isError {
		ma.errorCount++
	}
	ma.lastActivity = ev.Timestamp
	ma.lastLatency = ev.Latency
	ma.recentErrors[ma.recentIdx] = isError
	ma.recentIdx = (ma.recentIdx + 1) % recentWindow
	if ma.recentLen < recentWindow {
		ma.recentLen++
	}

	// Update provider stats.
	if providerName != "" {
		pa := sc.providers[providerName]
		if pa == nil {
			pa = &providerAccum{
				recentErrors: make([]bool, recentWindow),
			}
			sc.providers[providerName] = pa
		}
		pa.requestCount++
		if isError {
			pa.errorCount++
			pa.lastError = ev.Timestamp
			pa.lastErrorMessage = ev.ErrorMessage
		} else {
			pa.lastSuccess = ev.Timestamp
		}
		pa.recentErrors[pa.recentIdx] = isError
		pa.recentIdx = (pa.recentIdx + 1) % recentWindow
		if pa.recentLen < recentWindow {
			pa.recentLen++
		}
	}
}

// RecordFallback increments the fallback counter for a mapping. Called by the
// web layer or middleware when a fallback event is detected. This is separate
// from record() because the EventBus RequestEvent doesn't carry a
// primary-vs-fallback signal — the LastResponder handles that, and this
// method allows the StatsCollector to be notified externally.
//
// Safe to call on a nil receiver.
func (sc *StatsCollector) RecordFallback(mappingName string) {
	if sc == nil || mappingName == "" {
		return
	}
	sc.mu.Lock()
	ma := sc.mappings[mappingName]
	if ma == nil {
		ma = &mappingAccum{
			recentErrors: make([]bool, recentWindow),
		}
		sc.mappings[mappingName] = ma
	}
	ma.fallbackCount++
	sc.mu.Unlock()
}

// MappingSnapshot returns a copy of per-mapping stats safe for iteration
// without holding the lock.
func (sc *StatsCollector) MappingSnapshot() map[string]MappingStats {
	if sc == nil {
		return nil
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	out := make(map[string]MappingStats, len(sc.mappings))
	for name, ma := range sc.mappings {
		out[name] = MappingStats{
			RequestCount:    ma.requestCount,
			ErrorCount:      ma.errorCount,
			FallbackCount:   ma.fallbackCount,
			LastActivity:    ma.lastActivity,
			LastLatency:     ma.lastLatency,
			RecentErrorRate: computeErrorRate(ma.recentErrors, ma.recentLen),
		}
	}
	return out
}

// ProviderSnapshot returns a copy of per-provider stats safe for iteration
// without holding the lock.
func (sc *StatsCollector) ProviderSnapshot() map[string]ProviderStats {
	if sc == nil {
		return nil
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	out := make(map[string]ProviderStats, len(sc.providers))
	for name, pa := range sc.providers {
		out[name] = ProviderStats{
			RequestCount:     pa.requestCount,
			ErrorCount:       pa.errorCount,
			LastSuccess:      pa.lastSuccess,
			LastError:        pa.lastError,
			LastErrorMessage: pa.lastErrorMessage,
			RecentErrorRate:  computeErrorRate(pa.recentErrors, pa.recentLen),
		}
	}
	return out
}

// computeErrorRate calculates the error rate from a circular buffer of recent
// outcomes. Returns 0.0 when no data is available.
func computeErrorRate(buf []bool, length int) float64 {
	if length == 0 {
		return 0
	}
	errors := 0
	for i := 0; i < length; i++ {
		if buf[i] {
			errors++
		}
	}
	return float64(errors) / float64(length)
}
