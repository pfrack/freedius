package proxy

import (
	"net/http"

	"github.com/pfrack/freedius/config"
)

type Provider interface {
	Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error
}

type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers map[string]Provider) *Registry {
	for name, p := range providers {
		if p == nil {
			panic("proxy: nil provider for " + name)
		}
	}
	return &Registry{providers: providers}
}

func (r *Registry) Lookup(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}
