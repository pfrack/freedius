package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

// OpenAICompatibleAdapter translates Anthropic-format requests into the
// OpenAI Chat Completions format and streams the translated SSE response back.
type OpenAICompatibleAdapter struct {
	client        *http.Client
	logger        *slog.Logger
	streamTimeout time.Duration
}

// preSendHooks maps provider-declared pre_send_hook names to functions.
var preSendHooks = map[string]func([]byte) ([]byte, error){
	"sanitizeNIMBody": sanitizeNIMBody,
}

// NewOpenAICompatibleAdapter returns an adapter with the default stream
// timeout (5 minutes). Use NewOpenAICompatibleAdapterWithTimeout to override.
func NewOpenAICompatibleAdapter(logger *slog.Logger) *OpenAICompatibleAdapter {
	return NewOpenAICompatibleAdapterWithTimeout(logger, 5*time.Minute)
}

// NewOpenAICompatibleAdapterWithTimeout returns an adapter that aborts the
// upstream call after streamTimeout (per-request, via context.WithTimeout).
func NewOpenAICompatibleAdapterWithTimeout(
	logger *slog.Logger,
	streamTimeout time.Duration,
) *OpenAICompatibleAdapter {
	return &OpenAICompatibleAdapter{
		client: &http.Client{
			Timeout: 0, // bounded per-request via context.WithTimeout below
			Transport: &http.Transport{
				DisableKeepAlives: false,
				Proxy:             http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
		logger:        logger.With("component", "adapter.openai"),
		streamTimeout: streamTimeout,
	}
}

// Handle forwards the request to the OpenAI-compatible upstream and returns
// the reply in the wire format the client speaks. freedius is Anthropic-first:
// a client posting Anthropic Messages (POST /v1/messages) gets its request
// translated into OpenAI Chat Completions and the upstream event stream
// translated back; a client already speaking OpenAI (POST
// /v1/chat/completions) is forwarded untranslated so unknown fields survive
// and its chunk parser sees chunks. The body argument is the already-read
// request body.
func (a *OpenAICompatibleAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	provider config.Provider,
	mapping config.Mapping,
	body []byte,
) error {
	if provider.DefaultBaseURL == "" {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (openai-compat): missing base_url",
				mapping.ProviderName,
			),
			errType: "invalid_request_error",
		}
	}
	var apiKey string
	if provider.DefaultAPIKeyEnv != "" {
		apiKey = os.Getenv(provider.DefaultAPIKeyEnv)
		if apiKey == "" {
			return &configError{
				err: fmt.Errorf(
					"%s adapter (openai-compat): env var %s is not set",
					mapping.ProviderName,
					provider.DefaultAPIKeyEnv,
				),
				errType: "authentication_error",
			}
		}
	}
	opts := translate.Opts{}
	var hook func([]byte) ([]byte, error)
	if provider.OpenAI != nil {
		opts.NoStreamUsage = provider.OpenAI.NoStreamUsage
		opts.MaxTokensCap = provider.OpenAI.MaxTokensCap
		if provider.OpenAI.PreSendHook != "" {
			h, ok := preSendHooks[provider.OpenAI.PreSendHook]
			if !ok {
				return &configError{
					err: fmt.Errorf(
						"%s adapter (openai-compat): unknown pre_send_hook %q",
						mapping.ProviderName, provider.OpenAI.PreSendHook,
					),
					errType: "invalid_request_error",
				}
			}
			hook = h
		}
	}
	openAIClient := clientSpeaksOpenAI(r.URL.Path)
	streamed := requestWantsStream(body)
	var (
		upstreamBody []byte
		err          error
	)
	if openAIClient {
		upstreamBody, err = translate.Passthrough(body, mapping.ModelString, opts)
	} else {
		upstreamBody, err = translate.Request(body, mapping.ModelString, opts)
	}
	if err != nil {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (openai-compat): translate request: %w",
				mapping.ProviderName,
				err,
			),
			errType: "invalid_request_error",
		}
	}
	if hook != nil {
		upstreamBody, err = hook(upstreamBody)
		if err != nil {
			return &configError{
				err: fmt.Errorf(
					"%s adapter (openai-compat): sanitize body: %w",
					mapping.ProviderName,
					err,
				),
				errType: "invalid_request_error",
			}
		}
	}
	// Bound the upstream call so a hanging provider cannot pin the goroutine.
	// Cancellation still propagates via r.Context() to the upstream request.
	ctx, cancel := context.WithTimeout(r.Context(), a.streamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		provider.DefaultBaseURL,
		bytes.NewReader(upstreamBody),
	)
	if err != nil {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (openai-compat): build request: %w",
				mapping.ProviderName,
				err,
			),
			errType: "invalid_request_error",
		}
	}
	req.Header.Set("Content-Type", "application/json")
	if streamed {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", resolveUserAgent(r))
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		// Note: returning a plain error here makes the dispatcher classify
		// this as a 529 overloaded_error (fallback-eligible). The anthropic
		// adapter uses writeTransportError + returns nil instead. The two
		// adapters intentionally differ; align them only as a deliberate
		// contract change across both paths.
		return fmt.Errorf("%s adapter (openai-compat): do request: %w", mapping.ProviderName, err)
	}
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.StatusCode >= 400 {
		return classifyUpstreamError(resp)
	}

	// Extract the original request model from context to echo back in response.
	modelOverride := RequestModelFromContext(r.Context())
	if openAIClient {
		return a.forwardOpenAI(w, resp, modelOverride, streamed)
	}
	if !streamed {
		return a.forwardAnthropicMessage(w, resp, modelOverride)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	err = translate.Stream(resp.Body, w, rc.Flush, modelOverride)
	if err != nil {
		// Response already started; write error event in-band
		errPayload := map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "api_error",
				"message": err.Error(),
			},
		}
		errBytes, marshalErr := json.Marshal(errPayload)
		if marshalErr == nil {
			line := fmt.Sprintf("event: error\ndata: %s\n\n", errBytes)
			if _, writeErr := w.Write([]byte(line)); writeErr == nil {
				_ = rc.Flush()
			}
		}
		a.logger.Warn("stream translation error", "err", err)
	}
	return nil
}

// forwardOpenAI relays an OpenAI-protocol upstream response to an
// OpenAI-protocol client: chunks stay chunks, a JSON completion stays JSON, and
// the only change is the echoed model name. Streaming responses are forwarded
// event by event so each chunk is flushed as the upstream produces it. An
// upstream that streams although the client asked for a non-streaming reply is
// folded into one chat.completion rather than relayed as an event stream the
// client cannot parse.
func (a *OpenAICompatibleAdapter) forwardOpenAI(
	w http.ResponseWriter,
	resp *http.Response,
	modelOverride string,
	streamed bool,
) error {
	copyResponseHeaders(w.Header(), resp.Header)
	rc := http.NewResponseController(w)
	upstreamStreamed := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	if upstreamStreamed && !streamed {
		return a.forwardOpenAICompletion(w, resp, modelOverride)
	}

	if upstreamStreamed {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		if modelOverride == "" {
			_, _ = io.Copy(w, resp.Body)
			return nil
		}
		err := forwardSSEEchoingModel(
			w,
			bufio.NewReader(resp.Body),
			modelOverride,
			rc.Flush,
		)
		if err != nil && !errors.Is(err, io.EOF) {
			// Response already started; nothing left to do but record it.
			a.logger.Warn("openai sse forward error", "err", err)
		}
		return nil
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(MaxBodyBytes)+1))
	if err != nil {
		return &upstreamError{
			status:  502,
			errType: "api_error",
			message: "upstream response unreadable",
		}
	}
	if len(raw) > MaxBodyBytes {
		// Oversized bodies are relayed unchanged rather than retained in
		// memory for rewriting.
		raw = raw[:MaxBodyBytes]
	} else if modelOverride != "" {
		raw = rewriteTopLevelModel(raw, modelOverride)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(raw)
	return nil
}

// forwardOpenAICompletion answers a non-streaming OpenAI client with one
// chat.completion document, folding the upstream event stream into it.
func (a *OpenAICompatibleAdapter) forwardOpenAICompletion(
	w http.ResponseWriter,
	resp *http.Response,
	modelOverride string,
) error {
	raw, err := translate.Completion(resp.Body, modelOverride)
	if err != nil {
		a.logger.Warn("non-streaming translation error", "err", err)
		return &upstreamError{
			status:  502,
			errType: "api_error",
			message: "upstream response could not be translated",
		}
	}
	copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(raw)
	return nil
}

// forwardAnthropicMessage answers a non-streaming Anthropic Messages request
// with a single Message document. An upstream that answered with an event
// stream anyway is folded into one Message rather than relayed as SSE the
// caller cannot parse.
func (a *OpenAICompatibleAdapter) forwardAnthropicMessage(
	w http.ResponseWriter,
	resp *http.Response,
	modelOverride string,
) error {
	var (
		raw []byte
		err error
	)
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		raw, err = translate.Message(resp.Body, modelOverride)
	} else {
		var body []byte
		body, err = io.ReadAll(io.LimitReader(resp.Body, int64(MaxBodyBytes)+1))
		switch {
		case err != nil:
		case len(body) > MaxBodyBytes:
			// Oversized bodies are relayed unchanged rather than parsed.
			raw = body
		default:
			raw, err = translate.Response(body, modelOverride)
		}
	}
	if err != nil {
		a.logger.Warn("non-streaming translation error", "err", err)
		return &upstreamError{
			status:  502,
			errType: "api_error",
			message: "upstream response could not be translated",
		}
	}

	copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(raw)
	return nil
}

// clientSpeaksOpenAI reports whether the downstream client is speaking the
// OpenAI wire format rather than the Anthropic Messages API. freedius routes
// on the request path: /v1/chat/completions (or the legacy /v1/completions) is
// an OpenAI client, anything else is treated as an Anthropic client and
// translated. Anthropic clients that post to an OpenAI-shaped path would be
// misrouted, so the path is the contract, not the body shape.
func clientSpeaksOpenAI(path string) bool {
	trimmed := strings.TrimSuffix(path, "/")
	return strings.HasSuffix(trimmed, "/chat/completions") ||
		strings.HasSuffix(trimmed, "/completions")
}

// requestWantsStream reports whether the client asked for a streamed reply.
// The Anthropic and OpenAI wire formats both default stream to false.
func requestWantsStream(body []byte) bool {
	var probe struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Stream
}

// copyResponseHeaders copies upstream headers to dst, dropping the ones that
// describe a body freedius is about to rewrite or replace.
func copyResponseHeaders(dst, src http.Header) {
	for k, vv := range src {
		if strings.EqualFold(k, "Content-Length") ||
			strings.EqualFold(k, "Content-Encoding") ||
			strings.EqualFold(k, "Content-Type") {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
