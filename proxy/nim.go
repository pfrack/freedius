package proxy

import (
	"log/slog"
	"net/http"

	"github.com/pfrack/freedius/config"
)

type NIMAdapter struct {
	inner *OpenAICompatibleAdapter
}

func NewNIMAdapter(logger *slog.Logger) *NIMAdapter {
	return &NIMAdapter{inner: NewOpenAICompatibleAdapter(logger)}
}

func (a *NIMAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	return a.inner.Handle(w, r, m, body)
}
