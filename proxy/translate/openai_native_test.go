package translate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPassthrough_RewritesModelAndKeepsUnknownFields(t *testing.T) {
	body := []byte(`{"model":"kimi-k3","max_completion_tokens":4096,` +
		`"reasoning_effort":"high","response_format":{"type":"json_object"},` +
		`"parallel_tool_calls":false,"seed":9007199254740993,` +
		`"messages":[{"role":"user","content":"hi"}]}`)

	out, err := Passthrough(body, "upstream-model", Opts{})
	if err != nil {
		t.Fatalf("Passthrough: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if got["model"] != "upstream-model" {
		t.Errorf("model: got %v, want upstream-model", got["model"])
	}
	for _, key := range []string{
		"max_completion_tokens", "reasoning_effort", "response_format",
		"parallel_tool_calls", "seed", "messages",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("field %q dropped by passthrough: %s", key, out)
		}
	}
	// Large integers must survive verbatim instead of being rounded via float64.
	if !strings.Contains(string(out), "9007199254740993") {
		t.Errorf("large integer not preserved verbatim: %s", out)
	}
}

func TestPassthrough_ClampsMaxTokens(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		opts     Opts
		wantKey  string
		wantMax  any
		absentOK bool
	}{
		{
			name:    "max_tokens above cap is lowered",
			body:    `{"model":"m","max_tokens":5000}`,
			opts:    Opts{MaxTokensCap: 4096},
			wantKey: "max_tokens",
			wantMax: float64(4096),
		},
		{
			name:    "max_tokens below cap is kept",
			body:    `{"model":"m","max_tokens":100}`,
			opts:    Opts{MaxTokensCap: 4096},
			wantKey: "max_tokens",
			wantMax: float64(100),
		},
		{
			name:    "max_completion_tokens is clamped too",
			body:    `{"model":"m","max_completion_tokens":99999}`,
			opts:    Opts{MaxTokensCap: 8192},
			wantKey: "max_completion_tokens",
			wantMax: float64(8192),
		},
		{
			name:     "absent token field stays absent",
			body:     `{"model":"m"}`,
			opts:     Opts{MaxTokensCap: 4096},
			absentOK: true,
		},
		{
			name:    "no cap configured leaves the value alone",
			body:    `{"model":"m","max_tokens":5000}`,
			opts:    Opts{},
			wantKey: "max_tokens",
			wantMax: float64(5000),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Passthrough([]byte(tt.body), "m", tt.opts)
			if err != nil {
				t.Fatalf("Passthrough: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("result not JSON: %v", err)
			}
			if tt.absentOK {
				if _, ok := got["max_tokens"]; ok {
					t.Errorf("max_tokens should stay absent: %s", out)
				}
				return
			}
			if got[tt.wantKey] != tt.wantMax {
				t.Errorf("%s: got %v, want %v (%s)", tt.wantKey, got[tt.wantKey], tt.wantMax, out)
			}
		})
	}
}

func TestPassthrough_RejectsNonObjectBody(t *testing.T) {
	for _, body := range []string{`not json`, `[1,2,3]`, `"string"`} {
		if _, err := Passthrough([]byte(body), "m", Opts{}); err == nil {
			t.Errorf("body %q: expected error", body)
		}
	}
}

func TestResponse_TranslatesJSONCompletion(t *testing.T) {
	upstream := `{"id":"chatcmpl-7","object":"chat.completion","model":"upstream-model",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"Hello there"},` +
		`"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}}`

	out, err := Response([]byte(upstream), "client-model")
	if err != nil {
		t.Fatalf("Response: %v", err)
	}

	var got struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Role       string `json:"role"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if !strings.HasPrefix(got.ID, "msg_") {
		t.Errorf("id: got %q, want msg_-prefixed id", got.ID)
	}
	if got.Type != "message" || got.Role != "assistant" {
		t.Errorf("type/role: got %q/%q, want message/assistant", got.Type, got.Role)
	}
	if got.Model != "client-model" {
		t.Errorf("model: got %q, want client-model", got.Model)
	}
	if got.StopReason != "end_turn" {
		t.Errorf("stop_reason: got %q, want end_turn", got.StopReason)
	}
	if len(got.Content) != 1 || got.Content[0].Type != "text" ||
		got.Content[0].Text != "Hello there" {
		t.Errorf("content: got %+v", got.Content)
	}
	if got.Usage.Input != 11 || got.Usage.Output != 3 {
		t.Errorf("usage: got %+v", got.Usage)
	}
}

func TestResponse_TranslatesThinkingAndToolCalls(t *testing.T) {
	upstream := `{"id":"c1","model":"m","choices":[{"index":0,"message":{` +
		`"role":"assistant","content":"","reasoning_content":"pondering",` +
		`"tool_calls":[{"id":"call_1","type":"function",` +
		`"function":{"name":"get_weather","arguments":"{\"city\":\"sf\"}"}}]},` +
		`"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`

	out, err := Response([]byte(upstream), "")
	if err != nil {
		t.Fatalf("Response: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if got["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason: got %v, want tool_use", got["stop_reason"])
	}
	blocks, _ := got["content"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("want thinking + tool_use blocks, got: %s", out)
	}
	thinking, _ := blocks[0].(map[string]any)
	if thinking["type"] != "thinking" || thinking["thinking"] != "pondering" {
		t.Errorf("thinking block: got %v", thinking)
	}
	tool, _ := blocks[1].(map[string]any)
	if tool["type"] != "tool_use" || tool["id"] != "call_1" || tool["name"] != "get_weather" {
		t.Errorf("tool_use block: got %v", tool)
	}
	input, _ := tool["input"].(map[string]any)
	if input["city"] != "sf" {
		t.Errorf("tool input: got %v", tool["input"])
	}
}

func TestResponse_MalformedToolArgumentsArePreserved(t *testing.T) {
	upstream := `{"id":"c1","choices":[{"index":0,"message":{"role":"assistant",` +
		`"tool_calls":[{"id":"call_1","function":{"name":"f","arguments":"{\"a\":"}}]},` +
		`"finish_reason":"tool_calls"}]}`

	out, err := Response([]byte(upstream), "")
	if err != nil {
		t.Fatalf("Response: %v", err)
	}
	if !strings.Contains(string(out), `"_raw":"{\"a\":"`) {
		t.Errorf("truncated arguments should be preserved under _raw: %s", out)
	}
}

func TestResponse_GeneratesID(t *testing.T) {
	out, err := Response([]byte(`{"choices":[]}`), "client-model")
	if err != nil {
		t.Fatalf("Response: %v", err)
	}
	var got struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []any  `json:"content"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v", err)
	}
	if !strings.HasPrefix(got.ID, "msg_") {
		t.Errorf("id: got %q, want msg_ prefix", got.ID)
	}
	if got.StopReason != "end_turn" {
		t.Errorf("stop_reason: got %q, want end_turn", got.StopReason)
	}
	if len(got.Content) != 0 {
		t.Errorf("content: got %v, want empty", got.Content)
	}
}

func TestMessage_FoldsEventStreamIntoSingleDocument(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,` +
			`"model":"upstream-model","choices":[{"index":0,"delta":{"role":"assistant",` +
			`"reasoning_content":"think"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,` +
			`"model":"upstream-model","choices":[{"index":0,"delta":{"content":"Hel"},` +
			`"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,` +
			`"model":"upstream-model","choices":[{"index":0,"delta":{"content":"lo"},` +
			`"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-9","object":"chat.completion.chunk","created":1,` +
			`"model":"upstream-model","choices":[{"index":0,"delta":{},` +
			`"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":5,` +
			`"total_tokens":12}}`,
		"",
		"data: [DONE]",
		"",
		"",
	}, "\n")

	out, err := Message(strings.NewReader(upstream), "client-model")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}

	var got struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if !strings.HasPrefix(got.ID, "msg_") {
		t.Errorf("id: got %q, want msg_-prefixed id", got.ID)
	}
	if got.Model != "client-model" {
		t.Errorf("model: got %q, want client-model", got.Model)
	}
	if len(got.Content) != 2 {
		t.Fatalf("want thinking + text blocks, got: %s", out)
	}
	if got.Content[0].Type != "thinking" || got.Content[0].Thinking != "think" {
		t.Errorf("thinking block: got %+v", got.Content[0])
	}
	if got.Content[1].Type != "text" || got.Content[1].Text != "Hello" {
		t.Errorf("text block: got %+v", got.Content[1])
	}
	if got.Usage.Input != 7 || got.Usage.Output != 5 {
		t.Errorf("usage: got %+v", got.Usage)
	}
}

func TestMessage_AccumulatesStreamedToolArguments(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"tool_calls":[` +
			`{"index":0,"id":"call_1","function":{"name":"f","arguments":""}}]},` +
			`"finish_reason":null}]}`,
		"",
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"tool_calls":[` +
			`{"index":0,"function":{"arguments":"{\"a\":"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"tool_calls":[` +
			`{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{},` +
			`"finish_reason":"tool_calls"}]}`,
		"",
		"data: [DONE]",
		"",
		"",
	}, "\n")

	out, err := Message(strings.NewReader(upstream), "")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	var got struct {
		Content []struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if len(got.Content) != 1 {
		t.Fatalf("want one tool_use block, got: %s", out)
	}
	block := got.Content[0]
	if block.Type != "tool_use" || block.ID != "call_1" || block.Name != "f" {
		t.Errorf("tool block: got %+v", block)
	}
	if v, ok := block.Input["a"]; !ok || v.(float64) != 1 {
		t.Errorf("accumulated arguments: got %v", block.Input)
	}
}

func TestCompletion_FoldsEventStreamIntoOneDocument(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"chatcmpl-42","object":"chat.completion.chunk","created":7,` +
			`"model":"upstream-model","choices":[{"index":0,"delta":{"role":"assistant",` +
			`"reasoning_content":"hmm"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-42","object":"chat.completion.chunk","created":7,` +
			`"model":"upstream-model","choices":[{"index":0,"delta":{"content":"Hel"},` +
			`"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-42","object":"chat.completion.chunk","created":7,` +
			`"model":"upstream-model","choices":[{"index":0,"delta":{"content":"lo"},` +
			`"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-42","object":"chat.completion.chunk","created":7,` +
			`"model":"upstream-model","choices":[{"index":0,"delta":{},` +
			`"finish_reason":"stop"}],"usage":{"prompt_tokens":6,"completion_tokens":4,` +
			`"total_tokens":10}}`,
		"",
		"data: [DONE]",
		"",
		"",
	}, "\n")

	out, err := Completion(strings.NewReader(upstream), "client-model")
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}

	var got struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role             string `json:"role"`
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *openAIUsage `json:"usage"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if got.ID != "chatcmpl-42" || got.Object != "chat.completion" || got.Created != 7 {
		t.Errorf("completion identity lost: id=%q object=%q created=%d", got.ID, got.Object, got.Created)
	}
	if got.Model != "client-model" {
		t.Errorf("model: got %q, want client-model", got.Model)
	}
	if len(got.Choices) != 1 {
		t.Fatalf("want one choice, got: %s", out)
	}
	msg := got.Choices[0].Message
	if msg.Role != "assistant" || msg.Content != "Hello" || msg.ReasoningContent != "hmm" {
		t.Errorf("message: got %+v", msg)
	}
	if got.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason: got %q, want stop", got.Choices[0].FinishReason)
	}
	if got.Usage == nil || got.Usage.PromptTokens != 6 || got.Usage.CompletionTokens != 4 {
		t.Errorf("usage: got %+v", got.Usage)
	}
}

func TestCompletion_MergesStreamedToolCallFragments(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m",` +
			`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_9",` +
			`"type":"function","function":{"name":"f","arguments":""}}]},` +
			`"finish_reason":null}]}`,
		"",
		`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m",` +
			`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,` +
			`"function":{"arguments":"{\"k\":"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m",` +
			`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,` +
			`"function":{"arguments":"2}"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m",` +
			`"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		"",
		"data: [DONE]",
		"",
		"",
	}, "\n")

	out, err := Completion(strings.NewReader(upstream), "")
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	var got struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if got.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason: got %q", got.Choices[0].FinishReason)
	}
	calls := got.Choices[0].Message.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("want one merged tool call, got: %s", out)
	}
	if calls[0].ID != "call_9" || calls[0].Function.Name != "f" ||
		calls[0].Function.Arguments != `{"k":2}` {
		t.Errorf("tool call: got %+v", calls[0])
	}
}

func TestCompletion_EmptyStreamStillProducesAValidDocument(t *testing.T) {
	out, err := Completion(strings.NewReader("data: [DONE]\n\n"), "client-model")
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	var got struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if !strings.HasPrefix(got.ID, "chatcmpl-") {
		t.Errorf("id: got %q, want chatcmpl- prefix", got.ID)
	}
	if got.Object != "chat.completion" || got.Model != "client-model" {
		t.Errorf("object/model: got %q/%q", got.Object, got.Model)
	}
	if len(got.Choices) != 1 || got.Choices[0].Message.Role != "assistant" {
		t.Errorf("choices: got %+v", got.Choices)
	}
}

func TestCompletion_RejectsUnparseableChunk(t *testing.T) {
	if _, err := Completion(strings.NewReader("data: {oops\n\n"), ""); err == nil {
		t.Fatal("expected error for unparseable chunk")
	}
}

func TestMessage_RejectsNonJSONStreamChunk(t *testing.T) {
	_, err := Message(strings.NewReader("data: {not json\n\n"), "")
	if err == nil {
		t.Fatal("expected error for unparseable chunk")
	}
}
