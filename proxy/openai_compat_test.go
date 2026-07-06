package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

func newOpenAICompatibleAdapter(t *testing.T) *OpenAICompatibleAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewOpenAICompatibleAdapter(logger)
}

func TestOpenAICompat_Upstream401(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer upstream.Close()

	a := newOpenAICompatibleAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "openai",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "OPENAI_API_KEY",
		},
		config.Mapping{ProviderName: "openai", ModelString: "gpt-4"},
		[]byte(`{}`),
	)
	if err == nil {
		t.Fatal("expected upstreamError on 401")
	}
	var ue *upstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *upstreamError, got %T: %v", err, err)
	}
	if ue.status != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", ue.status, http.StatusUnauthorized)
	}
	if ue.errType != "authentication_error" {
		t.Errorf("errType: got %q, want authentication_error", ue.errType)
	}
	if rec.Body.Len() > 0 {
		t.Errorf("expected no bytes written to recorder, got body=%q", rec.Body.String())
	}
}

func TestOpenAICompat_MissingEnvVar(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	a := newOpenAICompatibleAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "openai",
			DefaultBaseURL:   "https://x",
			DefaultAPIKeyEnv: "OPENAI_API_KEY",
		},
		config.Mapping{ProviderName: "openai", ModelString: "gpt-4"},
		[]byte(`{}`),
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

// parseSSEEvents splits an SSE response body into individual events,
// each containing the "event:" line and "data:" payload.
type sseEvent struct {
	event string
	data  string
}

func parseSSEEvents(body string) []sseEvent {
	var events []sseEvent
	var cur sseEvent
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		case line == "" && (cur.event != "" || cur.data != ""):
			events = append(events, cur)
			cur = sseEvent{}
		}
	}
	if cur.event != "" || cur.data != "" {
		events = append(events, cur)
	}
	return events
}

func TestOpenAICompat_AnthropicResponseEnvelope(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(
			"data: {\"id\":\"chatcmpl-x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
		))
		_, _ = w.Write([]byte(
			"data: {\"id\":\"chatcmpl-x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n",
		))
		_, _ = w.Write([]byte(
			"data: {\"id\":\"chatcmpl-x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}}\n\n",
		))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newOpenAICompatibleAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"gpt-4","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "openai",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "OPENAI_API_KEY",
		},
		config.Mapping{ProviderName: "openai", ModelString: "gpt-4"},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", got)
	}

	events := parseSSEEvents(rec.Body.String())
	if len(events) == 0 {
		t.Fatal("no SSE events in response")
	}

	// Verify message_start event exists and has correct structure.
	var messageStart *sseEvent
	for i, ev := range events {
		if ev.event == "message_start" {
			messageStart = &events[i]
			break
		}
	}
	if messageStart == nil {
		t.Fatal("missing message_start event")
	}
	var startPayload map[string]any
	if err := json.Unmarshal([]byte(messageStart.data), &startPayload); err != nil {
		t.Fatalf("decode message_start data: %v", err)
	}
	if startPayload["type"] != "message_start" {
		t.Errorf("message_start type: got %v, want message_start", startPayload["type"])
	}
	msg, ok := startPayload["message"].(map[string]any)
	if !ok {
		t.Fatalf("message_start.message is not a map: %v", startPayload["message"])
	}
	if msg["type"] != "message" {
		t.Errorf("message.type: got %v, want message", msg["type"])
	}
	if msg["role"] != "assistant" {
		t.Errorf("message.role: got %v, want assistant", msg["role"])
	}

	// Verify message_stop event exists and has type field.
	var messageStop *sseEvent
	for i, ev := range events {
		if ev.event == "message_stop" {
			messageStop = &events[i]
			break
		}
	}
	if messageStop == nil {
		t.Fatal("missing message_stop event")
	}
	var stopPayload map[string]any
	if err := json.Unmarshal([]byte(messageStop.data), &stopPayload); err != nil {
		t.Fatalf("decode message_stop data: %v", err)
	}
	if stopPayload["type"] != "message_stop" {
		t.Errorf("message_stop type: got %v, want message_stop", stopPayload["type"])
	}

	// Verify message_delta event exists and has stop_reason.
	var messageDelta *sseEvent
	for i, ev := range events {
		if ev.event == "message_delta" {
			messageDelta = &events[i]
			break
		}
	}
	if messageDelta == nil {
		t.Fatal("missing message_delta event")
	}
	var deltaPayload map[string]any
	if err := json.Unmarshal([]byte(messageDelta.data), &deltaPayload); err != nil {
		t.Fatalf("decode message_delta data: %v", err)
	}
	if deltaPayload["type"] != "message_delta" {
		t.Errorf("message_delta type: got %v, want message_delta", deltaPayload["type"])
	}
	inner, ok := deltaPayload["delta"].(map[string]any)
	if !ok {
		t.Fatalf("message_delta.delta is not a map: %v", deltaPayload["delta"])
	}
	if inner["stop_reason"] != "end_turn" {
		t.Errorf("delta.stop_reason: got %v, want end_turn", inner["stop_reason"])
	}

	// Verify content_block_delta has text content.
	var contentDelta *sseEvent
	for i, ev := range events {
		if ev.event == "content_block_delta" {
			contentDelta = &events[i]
			break
		}
	}
	if contentDelta == nil {
		t.Fatal("missing content_block_delta event")
	}
	var cbPayload map[string]any
	if err := json.Unmarshal([]byte(contentDelta.data), &cbPayload); err != nil {
		t.Fatalf("decode content_block_delta data: %v", err)
	}
	if cbPayload["type"] != "content_block_delta" {
		t.Errorf("content_block_delta type: got %v, want content_block_delta", cbPayload["type"])
	}
}

func TestOpenAICompat_TranslationIncludesStream(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		if stream, ok := out["stream"].(bool); !ok || !stream {
			t.Errorf("upstream should see stream=true, got %v", out["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(
				"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n",
			),
		)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newOpenAICompatibleAdapter(t)
	rec := httptest.NewRecorder()
	body := []byte(
		`{"model":"x","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	err := a.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "openai",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "OPENAI_API_KEY",
		},
		config.Mapping{ProviderName: "openai", ModelString: "gpt-4"},
		body,
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
}
