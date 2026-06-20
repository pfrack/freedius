package proxy

import (
	"encoding/json"
	"net/http"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

// countTokensResponse is the Anthropic /v1/messages/count_tokens response
// envelope. Mirrors what Anthropic returns from the upstream endpoint:
//
//	{"input_tokens": N, "context_management": {"original_input_tokens": N}}
type countTokensResponse struct {
	InputTokens       int                           `json:"input_tokens"`
	ContextManagement *countTokensContextManagement `json:"context_management"`
}

type countTokensContextManagement struct {
	OriginalInputTokens int `json:"original_input_tokens"`
}

// serveLocalCountTokens runs the local BPE-based counter and writes a 200
// response in Anthropic format. Used when the resolved model routes to an
// OpenAI-protocol upstream that does not natively support count_tokens.
// The adapter is never invoked for this path — the local counter
// short-circuits the dispatch.
func (d *Dispatcher) serveLocalCountTokens(
	w http.ResponseWriter,
	r *http.Request,
	mapping config.Mapping,
	body []byte,
) {
	n, err := translate.CountInputTokens(body)
	if err != nil {
		d.Logger.Debug(
			"count_tokens: local count failed, returning 0",
			"request_id", RequestIDFromContext(r.Context()),
			"provider", mapping.ProviderName,
			"err", err,
		)
		n = 0
	}
	d.Logger.Debug(
		"count_tokens: local estimate",
		"request_id", RequestIDFromContext(r.Context()),
		"provider", mapping.ProviderName,
		"target_model", mapping.ModelString,
		"input_tokens", n,
	)
	resp := countTokensResponse{
		InputTokens: n,
		ContextManagement: &countTokensContextManagement{
			OriginalInputTokens: n,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.Logger.Error(
			"count_tokens: response encode failed",
			"request_id", RequestIDFromContext(r.Context()),
			"err", err,
		)
	}
}
