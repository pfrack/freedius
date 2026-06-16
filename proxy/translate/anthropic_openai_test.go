package translate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestTranslateRequestTextOnly(t *testing.T) {
	in := []byte(`{"model":"claude-opus-4","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "meta/llama-3.1-70b-instruct")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "meta/llama-3.1-70b-instruct" {
		t.Errorf("model: got %q", got.Model)
	}
	if *got.MaxTokens != 100 {
		t.Errorf("max_tokens: got %d", *got.MaxTokens)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages: got %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "hi" {
		t.Errorf("first message: got %+v", got.Messages[0])
	}
}

func TestTranslateRequestSystemString(t *testing.T) {
	in := []byte(`{"model":"claude-opus-4","max_tokens":100,"system":"You are helpful","messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "meta/llama")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages: got %d, want 2 (system + user)", len(got.Messages))
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "You are helpful" {
		t.Errorf("system message: got %+v", got.Messages[0])
	}
}

func TestTranslateRequestSystemBlocks(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"system":[{"type":"text","text":"You are"},{"type":"text","text":"helpful"}],"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "meta/llama")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Role != "system" {
		t.Fatalf("expected system role, got %+v", got.Messages[0])
	}
	if got.Messages[0].Content != "You are\nhelpful" {
		t.Errorf("system content: got %q, want %q", got.Messages[0].Content, "You are\nhelpful")
	}
}

func TestTranslateRequestToolUse(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"user","content":"weather?"},
		{"role":"assistant","content":[{"type":"text","text":"let me check"},{"type":"tool_use","id":"call_1","name":"get_weather","input":{"city":"Paris"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"sunny"}]}
	]}`)
	out, err := TranslateRequest(in, "meta/llama")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages: got %d, want 3", len(got.Messages))
	}
	if got.Messages[1].Role != "assistant" {
		t.Errorf("msg 1 role: got %q", got.Messages[1].Role)
	}
	if got.Messages[1].Content != "let me check" {
		t.Errorf("msg 1 content: got %q, want %q", got.Messages[1].Content, "let me check")
	}
	if len(got.Messages[1].ToolCalls) != 1 {
		t.Fatalf("msg 1 tool_calls: got %d, want 1", len(got.Messages[1].ToolCalls))
	}
	tc := got.Messages[1].ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "get_weather" {
		t.Errorf("tool call: got %+v", tc)
	}
	if tc.Function.Arguments != `{"city":"Paris"}` {
		t.Errorf("tool call arguments: got %q", tc.Function.Arguments)
	}
	if got.Messages[2].Role != "tool" {
		t.Errorf("msg 2 role: got %q, want tool", got.Messages[2].Role)
	}
	if got.Messages[2].ToolCallID != "call_1" {
		t.Errorf("tool_call_id: got %q", got.Messages[2].ToolCallID)
	}
	if got.Messages[2].Content != "sunny" {
		t.Errorf("tool result content: got %q", got.Messages[2].Content)
	}
}

func TestTranslateRequestParallelToolCalls(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"user","content":"weather in Paris and Tokyo?"},
		{"role":"assistant","content":[
			{"type":"tool_use","id":"call_1","name":"get_weather","input":{"city":"Paris"}},
			{"type":"tool_use","id":"call_2","name":"get_weather","input":{"city":"Tokyo"}}
		]}
	]}`)
	out, err := TranslateRequest(in, "meta/llama")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages[1].ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(got.Messages[1].ToolCalls))
	}
	if got.Messages[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("tc[0].id: got %q", got.Messages[1].ToolCalls[0].ID)
	}
	if got.Messages[1].ToolCalls[1].ID != "call_2" {
		t.Errorf("tc[1].id: got %q", got.Messages[1].ToolCalls[1].ID)
	}
}

func TestTranslateRequestImageBlock(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"user","content":[
			{"type":"text","text":"what's in this image?"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc123"}}
		]}
	]}`)
	out, err := TranslateRequest(in, "meta/llama")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	parts, ok := got.Messages[0].Content.([]any)
	if !ok {
		t.Fatalf("expected content parts, got %T", got.Messages[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	image, ok := parts[1].(map[string]any)
	if !ok {
		t.Fatalf("expected image map, got %T", parts[1])
	}
	if image["type"] != "image_url" {
		t.Errorf("image type: got %v", image["type"])
	}
}

func TestTranslateRequestStopSequences(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"stop_sequences":["STOP","END"],"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "meta/llama")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	arr, ok := got.Stop.([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("stop: got %+v, want array of 2", got.Stop)
	}
}

func TestTranslateRequestToolChoiceAuto(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"tool_choice":"auto","messages":[{"role":"user","content":"hi"}]}`)
	out, _ := TranslateRequest(in, "m")
	var got openAIRequest
	json.Unmarshal(out, &got)
	if got.ToolChoice != "auto" {
		t.Errorf("tool_choice: got %+v", got.ToolChoice)
	}
}

func TestTranslateRequestToolChoiceAny(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"tool_choice":"any","messages":[{"role":"user","content":"hi"}]}`)
	out, _ := TranslateRequest(in, "m")
	var got openAIRequest
	json.Unmarshal(out, &got)
	if got.ToolChoice != "required" {
		t.Errorf("tool_choice: got %+v, want required", got.ToolChoice)
	}
}

func TestTranslateRequestToolChoiceTool(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"tool_choice":{"type":"tool","name":"get_weather"},"messages":[{"role":"user","content":"hi"}]}`)
	out, _ := TranslateRequest(in, "m")
	var got openAIRequest
	json.Unmarshal(out, &got)
	tc, ok := got.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("tool_choice: got %T", got.ToolChoice)
	}
	if tc["type"] != "function" {
		t.Errorf("tool_choice.type: got %v", tc["type"])
	}
}

func TestTranslateRequestToolsUnwrap(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"tools":[
		{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}
	],"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tools: got %d", len(got.Tools))
	}
	if got.Tools[0].Type != "function" {
		t.Errorf("tool type: got %q", got.Tools[0].Type)
	}
	if got.Tools[0].Function.Name != "get_weather" {
		t.Errorf("tool name: got %q", got.Tools[0].Function.Name)
	}
	if got.Tools[0].Function.Parameters == nil {
		t.Error("tool parameters not unwrapped from input_schema")
	}
}

func TestTranslateRequestStreamAddsUsage(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Stream {
		t.Error("stream: got false, want true")
	}
	if got.StreamOptions == nil || !got.StreamOptions.IncludeUsage {
		t.Error("stream_options.include_usage: not set")
	}
}

func TestStreamTextOnly(t *testing.T) {
	upstream := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}

data: [DONE]

`
	var out bytes.Buffer
	flushCount := 0
	flush := func() error { flushCount++; return nil }
	if err := TranslateStream(strings.NewReader(upstream), &out, flush); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "event: message_start") {
		t.Errorf("missing message_start: %s", s)
	}
	if !strings.Contains(s, `"text_delta","text":"hello"`) {
		t.Errorf("missing first text_delta: %s", s)
	}
	if !strings.Contains(s, `"text_delta","text":" world"`) {
		t.Errorf("missing second text_delta: %s", s)
	}
	if !strings.Contains(s, `"stop_reason":"end_turn"`) {
		t.Errorf("missing stop_reason end_turn: %s", s)
	}
	if !strings.Contains(s, "event: message_stop") {
		t.Errorf("missing message_stop: %s", s)
	}
	if strings.Contains(s, "\n\n\n") {
		t.Errorf("output contains \\n\\n\\n (json.Encoder newline trap): %q", s)
	}
	if flushCount < 5 {
		t.Errorf("flush called %d times, want ≥ 5", flushCount)
	}
}

func TestStreamToolCall(t *testing.T) {
	upstream := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, `"type":"tool_use"`) {
		t.Errorf("missing tool_use block: %s", s)
	}
	if !strings.Contains(s, `"name":"get_weather"`) {
		t.Errorf("missing tool name: %s", s)
	}
	if !strings.Contains(s, `"partial_json":"{\"city\":"`) {
		t.Errorf("missing first partial_json: %s", s)
	}
	if !strings.Contains(s, `"partial_json":"\"Paris\"}"`) {
		t.Errorf("missing second partial_json: %s", s)
	}
	if !strings.Contains(s, `"stop_reason":"tool_use"`) {
		t.Errorf("missing stop_reason tool_use: %s", s)
	}
}

func TestStreamParallelToolCalls(t *testing.T) {
	upstream := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"a","arguments":""}},{"index":1,"id":"call_2","type":"function","function":{"name":"b","arguments":""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, `"name":"a"`) {
		t.Errorf("missing tool a: %s", s)
	}
	if !strings.Contains(s, `"name":"b"`) {
		t.Errorf("missing tool b: %s", s)
	}
	if strings.Count(s, "event: content_block_start") != 2 {
		t.Errorf("expected 2 content_block_start events, got %d in: %s", strings.Count(s, "event: content_block_start"), s)
	}
}

func TestStreamNoContent(t *testing.T) {
	upstream := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "event: message_start") {
		t.Errorf("missing message_start: %s", s)
	}
	if !strings.Contains(s, "event: message_stop") {
		t.Errorf("missing message_stop: %s", s)
	}
}

func TestStreamContentFilter(t *testing.T) {
	upstream := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"stop_reason":"refusal"`) {
		t.Errorf("expected refusal stop_reason: %s", out.String())
	}
}

func TestStreamUsageChunk(t *testing.T) {
	upstream := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":42,"total_tokens":52}}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"output_tokens":42`) {
		t.Errorf("expected output_tokens:42 in message_delta: %s", out.String())
	}
}

func TestStreamNoTripleNewline(t *testing.T) {
	upstream := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\n\n\n") {
		t.Errorf("output contains triple newline: %q", out.String())
	}
}

func TestTranslateRequestSystemMessage(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"system","content":"be brief"},
		{"role":"user","content":"hi"}
	]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Role != "system" {
		t.Errorf("expected system, got %+v", got.Messages[0])
	}
}

func TestTranslateRequestToolChoiceNone(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"tool_choice":"none","messages":[{"role":"user","content":"hi"}]}`)
	out, _ := TranslateRequest(in, "m")
	var got openAIRequest
	json.Unmarshal(out, &got)
	if got.ToolChoice != "none" {
		t.Errorf("tool_choice: got %+v", got.ToolChoice)
	}
}

func TestTranslateRequestToolResultStringContent(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"c1","content":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]}
		]}
	]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Content != "line1\nline2" {
		t.Errorf("tool_result content: got %q, want %q", got.Messages[0].Content, "line1\nline2")
	}
}

func TestTranslateRequestImageMissingFields(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"user","content":[
			{"type":"image","source":{}}
		]}
	]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	parts, _ := got.Messages[0].Content.([]any)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	p, _ := parts[0].(map[string]any)
	if p["type"] != "text" {
		t.Errorf("expected text fallback, got %v", p["type"])
	}
}

func TestTranslateRequestMalformed(t *testing.T) {
	_, err := TranslateRequest([]byte(`{not json`), "m")
	if err == nil {
		t.Error("expected error on malformed JSON, got nil")
	}
}

func TestStreamFlushError(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: [DONE]

`
	flushErr := fmt.Errorf("flush failed")
	flush := func() error { return flushErr }
	var out bytes.Buffer
	err := TranslateStream(strings.NewReader(upstream), &out, flush)
	if err == nil {
		t.Error("expected flush error to propagate, got nil")
	}
}

func TestStreamMalformedChunk(t *testing.T) {
	upstream := `data: not valid json

data: [DONE]

`
	var out bytes.Buffer
	err := TranslateStream(strings.NewReader(upstream), &out, nil)
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

func TestStreamFlushForMessageStop(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: [DONE]

`
	flushCount := 0
	flush := func() error { flushCount++; return nil }
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, flush); err != nil {
		t.Fatal(err)
	}
	if flushCount < 2 {
		t.Errorf("expected ≥ 2 flushes (events + message_stop), got %d", flushCount)
	}
}

func TestStreamMessageDeltaAtDoneNoUsage(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"stop_reason":"max_tokens"`) {
		t.Errorf("expected max_tokens stop_reason: %s", out.String())
	}
}

func TestStreamToolCallAlreadySeen(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"get_weather","arguments":"\"Paris\"}"}}]},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if strings.Count(body, "event: content_block_start") != 1 {
		t.Errorf("expected 1 content_block_start for tool, got %d: %s", strings.Count(body, "event: content_block_start"), body)
	}
	if !strings.Contains(body, `"name":"get_weather"`) {
		t.Errorf("missing tool name: %s", body)
	}
}

func TestStreamToolCallWithTextFirst(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"let me check "},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, `"type":"text"`) {
		t.Errorf("missing text block: %s", body)
	}
	if !strings.Contains(body, `"type":"tool_use"`) {
		t.Errorf("missing tool_use block: %s", body)
	}
	if strings.Count(body, "event: content_block_start") != 2 {
		t.Errorf("expected 2 content_block_start events, got %d: %s", strings.Count(body, "event: content_block_start"), body)
	}
}

func TestStreamUnrecognizedFinishReason(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"something_unusual"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"stop_reason":"end_turn"`) {
		t.Errorf("expected end_turn fallback for unknown reason: %s", out.String())
	}
}

func TestTranslateRequestToolChoiceMapAny(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"tool_choice":{"type":"any"},"messages":[{"role":"user","content":"hi"}]}`)
	out, _ := TranslateRequest(in, "m")
	var got openAIRequest
	json.Unmarshal(out, &got)
	if got.ToolChoice != "required" {
		t.Errorf("tool_choice: got %+v, want required", got.ToolChoice)
	}
}

func TestTranslateRequestNoSystemBlockText(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"system":[{"type":"text","text":""}],"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 {
		t.Errorf("expected only user message (no system), got %d messages", len(got.Messages))
	}
}

func TestStreamEmptyID(t *testing.T) {
	upstream := `data: {"id":"","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":"msg_freedius"`) {
		t.Errorf("expected fallback id msg_freedius: %s", out.String())
	}
}

func TestStreamEmitMessageStopFlushError(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: [DONE]

`
	calls := 0
	flush := func() error {
		calls++
		if calls == 2 {
			return fmt.Errorf("flush failed on stop")
		}
		return nil
	}
	var out bytes.Buffer
	err := TranslateStream(strings.NewReader(upstream), &out, flush)
	if err == nil {
		t.Error("expected error on stop flush failure")
	}
}

func TestStreamEmitMessageStopNilFlush(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "event: message_stop") {
		t.Errorf("missing message_stop: %s", out.String())
	}
}

func TestStreamFlushErrorOnPendingDelta(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	calls := 0
	flush := func() error {
		calls++
		if calls == 2 {
			return fmt.Errorf("flush failed on pending delta")
		}
		return nil
	}
	var out bytes.Buffer
	err := TranslateStream(strings.NewReader(upstream), &out, flush)
	if err == nil {
		t.Error("expected error on pending delta flush failure")
	}
}

func TestTranslateRequestUnknownRole(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[{"role":"developer","content":"hi"}]}`)
	_, err := TranslateRequest(in, "m")
	if err == nil {
		t.Error("expected error on unknown role")
	}
}

func TestTranslateRequestStringifyArray(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"c1","content":[{"type":"text","text":"a"}]}
		]}
	]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Content != "a" {
		t.Errorf("expected content 'a', got %q", got.Messages[0].Content)
	}
}

func TestStreamEncodeSSEError(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "event: message_start") {
		t.Errorf("missing message_start")
	}
}

func TestTranslateRequestToolChoiceUnknownMap(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"tool_choice":{"type":"other"},"messages":[{"role":"user","content":"hi"}]}`)
	out, _ := TranslateRequest(in, "m")
	var got openAIRequest
	json.Unmarshal(out, &got)
	if tc, ok := got.ToolChoice.(map[string]any); !ok || tc["type"] != "other" {
		t.Errorf("tool_choice: got %+v", got.ToolChoice)
	}
}

func TestTranslateRequestStringifyFallback(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[{"role":"system","content":42}]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Role != "system" {
		t.Errorf("expected system, got %+v", got.Messages[0])
	}
}

func TestTranslateRequestImageNoSource(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"user","content":[{"type":"image"}]}
	]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	parts, _ := got.Messages[0].Content.([]any)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
}

func TestStreamNilFlushDone(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "event: message_stop") {
		t.Errorf("missing message_stop: %s", out.String())
	}
}

func TestStreamContentBlockStopOnToolTransition(t *testing.T) {
	upstream := `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var out bytes.Buffer
	if err := TranslateStream(strings.NewReader(upstream), &out, nil); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if strings.Count(body, "event: content_block_stop") < 1 {
		t.Errorf("expected at least 1 content_block_stop (text->tool transition), got: %s", body)
	}
}

func TestTranslateRequestSystemNumber(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"system":42,"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 {
		t.Errorf("expected only user message, got %d", len(got.Messages))
	}
}

func TestTranslateRequestAssistantWithUnknownBlock(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"assistant","content":[
			{"type":"unknown_block","data":"foo"},
			{"type":"text","text":"hello"}
		]}
	]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Content != "hello" {
		t.Errorf("expected content 'hello' (unknown block skipped), got %q", got.Messages[0].Content)
	}
}

func TestTranslateRequestEmptySystemString(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"system":"","messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 {
		t.Errorf("expected only user message, got %d", len(got.Messages))
	}
}

func TestTranslateRequestSystemArrayWithString(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"system":["a string"],"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 {
		t.Errorf("expected only user message, got %d", len(got.Messages))
	}
}

func TestTranslateRequestUserWithNonMapBlock(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"user","content":["just a string"]}
	]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
}

func TestTranslateRequestUserUnknownType(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[{"role":"user","content":true}]}`)
	_, err := TranslateRequest(in, "m")
	if err == nil {
		t.Error("expected error on unsupported user content type")
	}
}

func TestTranslateRequestAssistantUnknownType(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[{"role":"assistant","content":42}]}`)
	_, err := TranslateRequest(in, "m")
	if err == nil {
		t.Error("expected error on unsupported assistant content type")
	}
}

func TestTranslateRequestToolUseNilInput(t *testing.T) {
	in := []byte(`{"model":"x","max_tokens":100,"messages":[
		{"role":"assistant","content":[
			{"type":"tool_use","id":"c1","name":"f"}
		]}
	]}`)
	out, err := TranslateRequest(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got openAIRequest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].ToolCalls[0].Function.Arguments != "" {
		t.Errorf("expected empty arguments, got %q", got.Messages[0].ToolCalls[0].Function.Arguments)
	}
}
