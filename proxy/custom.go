package proxy

import (
	"log/slog"
	"net/http"

	"github.com/pfrack/freedius/config"
)

type CustomAdapter struct {
	inner *AnthropicCompatibleAdapter
}

func NewCustomAdapter(logger *slog.Logger) *CustomAdapter {
	return &CustomAdapter{inner: NewAnthropicCompatibleAdapter(logger)}
}

func (a *CustomAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	return a.inner.Handle(w, r, m, body)
}
