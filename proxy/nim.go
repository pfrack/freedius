package proxy

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

// NIMAdapter wraps OpenAICompatibleAdapter with NVIDIA NIM-specific options:
// stream-usage is suppressed and the request body is run through
// sanitizeNIMBody before being sent.
type NIMAdapter struct {
	inner *OpenAICompatibleAdapter
}

// NewNIMAdapter returns a `nim` provider adapter. The actual
// request/response logic lives in OpenAICompatibleAdapter
// (proxy/openai_compat.go); this wrapper enables NIM-specific
// options (NoStreamUsage, sanitizeNIMBody pre-send hook).
func NewNIMAdapter(logger *slog.Logger, streamTimeout time.Duration) *NIMAdapter {
	inner := NewOpenAICompatibleAdapterWithTimeout(logger, streamTimeout)
	inner.translateOpts = translate.Opts{NoStreamUsage: true}
	inner.preSendHook = sanitizeNIMBody
	return &NIMAdapter{inner: inner}
}

// Handle delegates to the embedded OpenAICompatibleAdapter.
func (a *NIMAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	m config.Model,
	body []byte,
) error {
	return a.inner.Handle(w, r, m, body)
}
