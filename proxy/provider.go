package proxy

import (
	"fmt"
	"net/http"

	"github.com/pfrack/freedius/config"
)

// Provider is a single backend implementation that can serve a freedius
// request end-to-end (build upstream request, copy/stream the response).
type Provider interface {
	Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error
}

// Registry maps provider names (e.g. "nim", "openai", "anthropic") to their
// concrete Provider implementation.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry builds a Registry from the given map. It panics if any value
// is nil, since a nil provider would crash later at request time and the
// configuration error should surface at startup instead.
func NewRegistry(providers map[string]Provider) *Registry {
	for name, p := range providers {
		if p == nil {
			panic(fmt.Sprintf("proxy: nil provider registered for %q", name))
		}
	}
	return &Registry{providers: providers}
}

// Lookup returns the Provider registered under name, plus a boolean indicating
// whether such a provider exists.
func (r *Registry) Lookup(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}
