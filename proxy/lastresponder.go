package proxy

import (
	"sync"
	"time"
)

// lastResponderTTL bounds how long a recorded responder is treated as
// authoritative. Older entries are treated as "no responder" at read time
// (lazy TTL — no background goroutine required).
const lastResponderTTL = 60 * time.Second

// LastResponder is a per-mapping responder index aggregator. The dispatch
// path calls Record(mappingName, responderIndex) when a fallback chain
// succeeds on a non-primary entry. The web layer calls Lookup or Snapshot
// to render the chevron on the responder step.
//
// TTL eviction is lazy: callers compare time.Since(ts) at read time and drop
// expired entries on contact. No background goroutine is required. The clock
// is injectable so tests can advance virtual time without sleeping.
//
// Concurrency: all fields are guarded by mu. Every read of l.now — including
// the function-pointer read and the call itself — happens inside the locked
// region of Record/Lookup/Snapshot. SetClock also holds mu while writing the
// pointer, so there is no race between SetClock and the readers.
type LastResponder struct {
	mu      sync.Mutex
	entries map[string]lastResponderEntry
	now     func() time.Time
}

type lastResponderEntry struct {
	responder int
	ts        time.Time
}

// NewLastResponder returns a fresh aggregator backed by real time.
func NewLastResponder() *LastResponder {
	return &LastResponder{
		entries: make(map[string]lastResponderEntry),
		now:     time.Now,
	}
}

// SetClock replaces the time source. Intended for tests.
func (l *LastResponder) SetClock(now func() time.Time) {
	l.mu.Lock()
	l.now = now
	l.mu.Unlock()
}

// Record marks `responder` (0=primary, 1+=fallback index) as the most recent
// successful step for `mappingName`. Safe to call on a nil receiver.
func (l *LastResponder) Record(mappingName string, responder int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.entries[mappingName] = lastResponderEntry{
		responder: responder,
		ts:        l.now(),
	}
	l.mu.Unlock()
}

// Lookup returns (responderIndex, true) when a recent entry exists, or
// (0, false) when the entry is missing or expired. Expired entries are
// dropped on read to bound map growth. Safe to call on a nil receiver.
func (l *LastResponder) Lookup(mappingName string) (int, bool) {
	if l == nil {
		return 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[mappingName]
	if !ok {
		return 0, false
	}
	if l.now().Sub(e.ts) > lastResponderTTL {
		delete(l.entries, mappingName)
		return 0, false
	}
	return e.responder, true
}

// Snapshot returns a copy of the responder map with expired entries filtered
// out. Expired entries are also deleted from the underlying map so a Snapshot
// call alone is enough to bound map growth — callers do not need to drive
// Lookup to evict. Used by the web endpoint to render the chevron set. Safe
// to call on a nil receiver (returns nil).
func (l *LastResponder) Snapshot() map[string]int {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.entries))
	now := l.now()
	for k, e := range l.entries {
		if now.Sub(e.ts) > lastResponderTTL {
			delete(l.entries, k)
			continue
		}
		out[k] = e.responder
	}
	return out
}
