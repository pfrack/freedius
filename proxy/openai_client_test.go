package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pfrack/freedius/config"
)

func TestClientSpeaksOpenAI(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/v1/chat/completions", want: true},
		{path: "/chat/completions", want: true},
		{path: "/v1/chat/completions/", want: true},
		{path: "/api/v1/chat/completions", want: true},
		{path: "/v1/completions", want: true},
		{path: "/v1/messages", want: false},
		{path: "/v1/messages/count_tokens", want: false},
		{path: "/", want: false},
	}
	for _, tt := range tests {
		if got := clientSpeaksOpenAI(tt.path); got != tt.want {
			t.Errorf("clientSpeaksOpenAI(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestRequestWantsStream(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "stream true", body: `{"model":"m","stream":true}`, want: true},
		{name: "stream false", body: `{"model":"m","stream":false}`, want: false},
		{name: "stream omitted", body: `{"model":"m"}`, want: false},
		{name: "malformed", body: `{"model":`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestWantsStream([]byte(tt.body)); got != tt.want {
				t.Errorf("requestWantsStream(%s) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

// requestModel reads the "model" field out of a request body.
func requestModel(body string) string {
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(body), &probe); err != nil {
		return ""
	}
	return probe.Model
}

// openAIUpstream serves a fixed reply and captures the request body it received.
func openAIUpstream(
	t *testing.T,
	contentType, reply string,
	capture *[]byte,
	mu *sync.Mutex,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		*capture = body
		mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, reply)
	}))
}

func handleOpenAIAdapter(
	t *testing.T,
	adapter *OpenAICompatibleAdapter,
	path, body string,
	upstreamURL string,
) *httptest.ResponseRecorder {
	t.Helper()
	t.Setenv("OPENAI_CLIENT_TEST_KEY", "sk-test")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	// The dispatcher stores the client model in the request context; adapters
	// read it from there to echo the model back to the client.
	req = req.WithContext(WithRequestModel(req.Context(), requestModel(body)))
	err := adapter.Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "openai",
			DefaultBaseURL:   upstreamURL,
			DefaultAPIKeyEnv: "OPENAI_CLIENT_TEST_KEY",
		},
		config.Mapping{ProviderName: "openai", ModelString: "upstream-model"},
		[]byte(body),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	return rec
}

// An OpenAI client must receive OpenAI chunks: a chunk struct with a
// top-level "id", no Anthropic event framing. This is the regression that made
// an OpenAI-format client fail with `missing field 'id'`.
func TestOpenAIAdapter_OpenAIClientGetsOpenAIChunks(t *testing.T) {
	reply := "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\"," +
		"\"created\":1,\"model\":\"upstream-model\",\"choices\":[{\"index\":0," +
		"\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"
	var captured []byte
	var mu sync.Mutex
	upstream := openAIUpstream(t, "text/event-stream", reply, &captured, &mu)
	defer upstream.Close()

	body := `{"model":"kimi-k3","stream":true,"max_completion_tokens":4096,` +
		`"reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`
	rec := handleOpenAIAdapter(
		t, newOpenAIAdapterForTest(t), "/v1/chat/completions", body, upstream.URL,
	)

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}
	if strings.Contains(rec.Body.String(), "event: message_start") {
		t.Errorf("OpenAI client received Anthropic events: %s", rec.Body.String())
	}
	var chunk struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Content string `json:"-"`
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
		Model string `json:"model"`
	}
	line := strings.SplitN(strings.TrimPrefix(rec.Body.String(), "data: "), "\n", 2)[0]
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		t.Fatalf("first data line is not an OpenAI chunk: %v\n%s", err, rec.Body.String())
	}
	if chunk.ID != "chatcmpl-1" || chunk.Object != "chat.completion.chunk" {
		t.Errorf("chunk identity lost: id=%q object=%q", chunk.ID, chunk.Object)
	}
	if len(chunk.Choices) != 1 || chunk.Choices[0].Delta.Content != "hi" {
		t.Errorf("chunk content lost: %+v", chunk.Choices)
	}
	if chunk.Model != "kimi-k3" {
		t.Errorf("echoed model: got %q, want kimi-k3", chunk.Model)
	}
	if !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Errorf("[DONE] sentinel not forwarded: %s", rec.Body.String())
	}

	mu.Lock()
	upstreamBody := string(captured)
	mu.Unlock()
	var sent map[string]any
	if err := json.Unmarshal([]byte(upstreamBody), &sent); err != nil {
		t.Fatalf("upstream body not JSON: %v\n%s", err, upstreamBody)
	}
	if sent["model"] != "upstream-model" {
		t.Errorf("upstream model: got %v, want upstream-model", sent["model"])
	}
	if _, ok := sent["reasoning_effort"]; !ok {
		t.Errorf("provider-specific field dropped: %s", upstreamBody)
	}
	if _, ok := sent["stream_options"]; ok {
		t.Errorf("passthrough must not invent stream_options: %s", upstreamBody)
	}
}

func TestOpenAIAdapter_OpenAIClientGetsJSONCompletion(t *testing.T) {
	reply := `{"id":"chatcmpl-2","object":"chat.completion","created":1,` +
		`"model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant",` +
		`"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,` +
		`"completion_tokens":1,"total_tokens":3}}`
	var captured []byte
	var mu sync.Mutex
	upstream := openAIUpstream(t, "application/json", reply, &captured, &mu)
	defer upstream.Close()

	rec := handleOpenAIAdapter(
		t,
		newOpenAIAdapterForTest(t),
		"/v1/chat/completions",
		`{"model":"kimi-k3","stream":false,"messages":[{"role":"user","content":"hi"}]}`,
		upstream.URL,
	)

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
	var got struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, rec.Body.String())
	}
	if got.ID != "chatcmpl-2" {
		t.Errorf("id: got %q, want chatcmpl-2", got.ID)
	}
	if got.Model != "kimi-k3" {
		t.Errorf("echoed model: got %q, want kimi-k3", got.Model)
	}
	if len(got.Choices) != 1 || got.Choices[0].Message.Content != "hello" {
		t.Errorf("completion lost: %+v", got.Choices)
	}
}

func TestOpenAIAdapter_AnthropicClientNonStreamingGetsMessage(t *testing.T) {
	reply := `{"id":"chatcmpl-3","object":"chat.completion","model":"upstream-model",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},` +
		`"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,` +
		`"total_tokens":6}}`
	var captured []byte
	var mu sync.Mutex
	upstream := openAIUpstream(t, "application/json", reply, &captured, &mu)
	defer upstream.Close()

	rec := handleOpenAIAdapter(
		t,
		newOpenAIAdapterForTest(t),
		"/v1/messages",
		`{"model":"claude-opus-4","max_tokens":32,"stream":false,`+
			`"messages":[{"role":"user","content":"hi"}]}`,
		upstream.URL,
	)

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
	var got struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Model   string `json:"model"`
		Content []any  `json:"content"`
		Usage   struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, rec.Body.String())
	}
	if got.Type != "message" || got.Role != "assistant" {
		t.Errorf("not an Anthropic Message: %+v", got)
	}
	if got.Model != "claude-opus-4" {
		t.Errorf("echoed model: got %q, want claude-opus-4", got.Model)
	}
	if got.StopReason != "end_turn" {
		t.Errorf("stop_reason: got %q, want end_turn", got.StopReason)
	}
	if len(got.Content) != 1 {
		t.Fatalf("content: got %v, want one text block", got.Content)
	}
	block, _ := got.Content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "hi there" {
		t.Errorf("content block: got %v", block)
	}
	if got.Usage.Input != 4 {
		t.Errorf("usage: got %+v", got.Usage)
	}
}

// A non-streaming caller must still get its answer when the upstream streams
// anyway: the event stream is folded into one Message, not relayed as SSE.
func TestOpenAIAdapter_AnthropicClientNonStreamingFoldsUpstreamStream(t *testing.T) {
	reply := "data: {\"id\":\"chatcmpl-4\",\"object\":\"chat.completion.chunk\"," +
		"\"created\":1,\"model\":\"upstream-model\",\"choices\":[{\"index\":0," +
		"\"delta\":{\"role\":\"assistant\",\"content\":\"str\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-4\",\"object\":\"chat.completion.chunk\"," +
		"\"created\":1,\"model\":\"upstream-model\",\"choices\":[{\"index\":0," +
		"\"delta\":{\"content\":\"eamed\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-4\",\"object\":\"chat.completion.chunk\"," +
		"\"created\":1,\"model\":\"upstream-model\",\"choices\":[{\"index\":0," +
		"\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1," +
		"\"completion_tokens\":1,\"total_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	var captured []byte
	var mu sync.Mutex
	upstream := openAIUpstream(t, "text/event-stream", reply, &captured, &mu)
	defer upstream.Close()

	rec := handleOpenAIAdapter(
		t,
		newOpenAIAdapterForTest(t),
		"/v1/messages",
		`{"model":"claude-opus-4","max_tokens":32,"stream":false,`+
			`"messages":[{"role":"user","content":"hi"}]}`,
		upstream.URL,
	)

	if strings.Contains(rec.Body.String(), "event:") {
		t.Errorf("SSE framing leaked into a non-streaming reply: %s", rec.Body.String())
	}
	var got struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, rec.Body.String())
	}
	if got.Type != "message" {
		t.Errorf("type: got %q, want message", got.Type)
	}
	if len(got.Content) != 1 || got.Content[0].Text != "streamed" {
		t.Errorf("folded content: got %+v", got.Content)
	}
}

func TestOpenAIAdapter_NonStreamingUpstreamFailureStaysFallbackEligible(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}))
	defer upstream.Close()

	t.Setenv("OPENAI_CLIENT_TEST_KEY", "sk-test")
	rec := httptest.NewRecorder()
	body := `{"model":"claude-opus-4","max_tokens":8,"stream":false,` +
		`"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(body)))
	req = req.WithContext(WithRequestModel(req.Context(), requestModel(body)))
	err := newOpenAIAdapterForTest(t).Handle(
		rec,
		req,
		config.Provider{
			Behavior:         "openai",
			DefaultBaseURL:   upstream.URL,
			DefaultAPIKeyEnv: "OPENAI_CLIENT_TEST_KEY",
		},
		config.Mapping{ProviderName: "openai", ModelString: "upstream-model"},
		[]byte(body),
	)
	if err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
	var got struct {
		Type    string `json:"type"`
		Content []any  `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, rec.Body.String())
	}
	if got.Type != "message" {
		t.Errorf("type: got %q, want message", got.Type)
	}
	if len(got.Content) != 1 {
		t.Errorf("content: got %v, want one text block", got.Content)
	}
}
