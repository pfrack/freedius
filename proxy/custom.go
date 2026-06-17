package proxy

import (
	"log/slog"
	"net/http"

	"github.com/pfrack/freedius/config"
)

type CustomAdapter struct {
	inner *AnthropicCompatibleAdapter
}

// NewCustomAdapter returns a `custom` provider adapter. The actual
// request/response logic lives in AnthropicCompatibleAdapter
// (proxy/anthropic_compat.go); this wrapper exists so the registry
// can key `custom` separately and apply custom-only configuration.

func NewCustomAdapter(logger *slog.Logger, verboseErrors bool) *CustomAdapter {
	return &CustomAdapter{inner: NewAnthropicCompatibleAdapter(logger, verboseErrors)}
}

func (a *CustomAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	m config.Model,
	body []byte,
) error {
	return a.inner.Handle(w, r, m, body)
}
