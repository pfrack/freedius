package proxy

import (
	"log/slog"
	"net/http"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

type NIMAdapter struct {
	inner *OpenAICompatibleAdapter
}

func NewNIMAdapter(logger *slog.Logger) *NIMAdapter {
	inner := NewOpenAICompatibleAdapter(logger)
	inner.translateOpts = translate.TranslateOpts{NoStreamUsage: true}
	inner.preSendHook = sanitizeNIMBody
	return &NIMAdapter{inner: inner}
}

func (a *NIMAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	return a.inner.Handle(w, r, m, body)
}
