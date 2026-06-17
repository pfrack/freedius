package proxy

import (
	"net/http"
	"testing"

	"github.com/pfrack/freedius/config"
)

type stubProvider struct {
	called bool
	err    error
}

func (s *stubProvider) Handle(
	w http.ResponseWriter,
	r *http.Request,
	m config.Model,
	body []byte,
) error {
	s.called = true
	if s.err != nil {
		w.WriteHeader(http.StatusBadGateway)
	}
	return s.err
}

func TestNewRegistry_NilProviderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil provider")
		}
	}()
	NewRegistry(map[string]Provider{
		"nim": nil,
	})
}

func TestNewRegistry_ValidProviders(t *testing.T) {
	stub := &stubProvider{}
	r := NewRegistry(map[string]Provider{"nim": stub, "custom": stub})
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
}

func TestRegistry_Lookup(t *testing.T) {
	stub := &stubProvider{}
	r := NewRegistry(map[string]Provider{"nim": stub})

	p, ok := r.Lookup("nim")
	if !ok {
		t.Fatal("expected to find nim")
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}

	if _, ok := r.Lookup("missing"); ok {
		t.Fatal("expected ok=false for missing provider")
	}
}
