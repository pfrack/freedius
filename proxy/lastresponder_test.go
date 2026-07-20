package proxy

import (
	"testing"
	"time"
)

func TestLastResponder_AggregationAndLookup(t *testing.T) {
	lr := NewLastResponder()
	lr.Record("q", 0)
	lr.Record("q", 2)
	lr.Record("other", 1)

	if got, ok := lr.Lookup("q"); !ok || got != 2 {
		t.Errorf("Lookup(q) = (%d, %v), want (2, true)", got, ok)
	}
	if got, ok := lr.Lookup("other"); !ok || got != 1 {
		t.Errorf("Lookup(other) = (%d, %v), want (1, true)", got, ok)
	}
	if _, ok := lr.Lookup("missing"); ok {
		t.Errorf("Lookup(missing) returned ok=true")
	}

	snap := lr.Snapshot()
	if snap["q"] != 2 || snap["other"] != 1 {
		t.Errorf("Snapshot = %v, want {q:2, other:1}", snap)
	}
}

func TestLastResponder_TTLEvicts(t *testing.T) {
	lr := NewLastResponder()
	base := time.Now()
	lr.SetClock(func() time.Time { return base })
	lr.Record("q", 1)

	// Advance virtual clock past the TTL window.
	lr.SetClock(func() time.Time { return base.Add(2 * lastResponderTTL) })

	if _, ok := lr.Lookup("q"); ok {
		t.Errorf("Lookup should return ok=false after TTL elapses")
	}
	if got := lr.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot should drop expired entries; got %v", got)
	}
}

func TestLastResponder_NilSafe(t *testing.T) {
	var lr *LastResponder // nil
	lr.Record("q", 0)
	if _, ok := lr.Lookup("q"); ok {
		t.Errorf("nil receiver should return ok=false")
	}
	if got := lr.Snapshot(); len(got) != 0 {
		t.Errorf("nil receiver Snapshot should be empty; got %v", got)
	}
}
