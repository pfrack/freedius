package translate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRequest_TextOnly(t *testing.T) {
	in := []byte(`{
		"model":"claude-opus-4",
		"max_tokens":50,
		"messages":[{"role":"user","content":"hello"}],
		"stream":true
	}`)
	out, err := Request(in, "meta-llama", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "meta-llama" {
		t.Errorf("model: got %v, want meta-llama", got["model"])
	}
	if got["stream"] != true {
		t.Errorf("stream: got %v, want true", got["stream"])
	}
	streamOpts, ok := got["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("stream_options missing or wrong type: %v", got["stream_options"])
	}
	if streamOpts["include_usage"] != true {
		t.Errorf("stream_options.include_usage: got %v, want true", streamOpts["include_usage"])
	}
	msgs, ok := got["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages: got %v", got["messages"])
	}
	if msgs[0].(map[string]any)["role"] != "user" {
		t.Errorf("messages[0].role: got %v, want user", msgs[0].(map[string]any)["role"])
	}
}

func TestRequest_SystemAndMessages(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"system":"You are a helpful assistant",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"user","content":"how are you?"}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (system + 3), got %d", len(msgs))
	}
	if msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("messages[0].role: got %v, want system", msgs[0].(map[string]any)["role"])
	}
}

func TestRequest_ToolUse(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":100,
		"messages":[
			{"role":"user","content":"what's the weather?"},
			{"role":"assistant","content":[
				{"type":"tool_use","id":"call_1","name":"get_weather","input":{"city":"sf"}}
			]}
		],
		"tools":[
			{"name":"get_weather","description":"Get the weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}
		],
		"tool_choice":"auto"
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	tools := got["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools: got %v", got["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool.type: got %v", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("tool.function.name: got %v", fn["name"])
	}
	if got["tool_choice"] != "auto" {
		t.Errorf("tool_choice: got %v, want auto", got["tool_choice"])
	}
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Errorf("assistant role: got %v", assistant["role"])
	}
	tcs, ok := assistant["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Errorf("expected tool_calls array, got %v", assistant["tool_calls"])
	}
}

func TestRequest_ToolResult(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":50,
		"messages":[
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"sunny"}]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	tool := msgs[0].(map[string]any)
	if tool["role"] != "tool" {
		t.Errorf("role: got %v, want tool", tool["role"])
	}
	if tool["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id: got %v", tool["tool_call_id"])
	}
}

func TestRequest_StopSequences(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"stop_sequences":["END"],
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["stop"] != "END" {
		t.Errorf("stop: got %v, want END", got["stop"])
	}
}

func TestStream_TextStream(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hello "},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"world"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}

data: [DONE]

`
	var downstream bytes.Buffer
	flushes := 0
	flush := func() error { flushes++; return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, "event: message_start") {
		t.Errorf("missing message_start: %q", out)
	}
	if !strings.Contains(out, "event: content_block_start") {
		t.Errorf("missing content_block_start: %q", out)
	}
	if !strings.Contains(out, "hello ") {
		t.Errorf("missing first text delta: %q", out)
	}
	if !strings.Contains(out, "world") {
		t.Errorf("missing second text delta: %q", out)
	}
	if !strings.Contains(out, "event: content_block_stop") {
		t.Errorf("missing content_block_stop: %q", out)
	}
	if !strings.Contains(out, "event: message_delta") {
		t.Errorf("missing message_delta: %q", out)
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Errorf("missing message_stop: %q", out)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Errorf("output contains triple newline (json.Encoder trap): %q", out)
	}
	if flushes < 5 {
		t.Errorf("expected at least 5 flushes, got %d", flushes)
	}
}

func TestStream_ToolCall(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\""}}]},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"sf\"}"}}]},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"type":"tool_use"`) {
		t.Errorf("missing tool_use content_block: %q", out)
	}
	if !strings.Contains(out, `"name":"get_weather"`) {
		t.Errorf("missing tool name: %q", out)
	}
	if !strings.Contains(out, `"type":"input_json_delta"`) {
		t.Errorf("missing input_json_delta: %q", out)
	}
	if !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Errorf("missing tool_use stop_reason: %q", out)
	}
}

func TestStream_ContentFilterFinishReason(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"x"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"stop_reason":"refusal"`) {
		t.Errorf("expected refusal stop_reason, got: %q", out)
	}
}

func TestStream_LengthFinishReason(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"x"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"stop_reason":"max_tokens"`) {
		t.Errorf("expected max_tokens stop_reason, got: %q", out)
	}
}

func TestStream_UsageOnlyChunk(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"output_tokens":7`) {
		t.Errorf("expected output_tokens:7 in message_delta, got: %q", out)
	}
}

func TestStream_JustDone(t *testing.T) {
	upstream := "data: [DONE]\n\n"
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(downstream.String(), "event: message_stop") {
		t.Errorf("expected message_stop on [DONE]-only, got: %q", downstream.String())
	}
}

func TestStream_DoubleDone_Idempotent(t *testing.T) {
	upstream := "data: [DONE]\n\ndata: [DONE]\n\n"
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
}

func TestStream_NonDataLine_Ignored(t *testing.T) {
	upstream := "event: ping\ndata: {\"event\":\"ping\"}\n\n" +
		"data: [DONE]\n\n"
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(downstream.String(), "event: message_stop") {
		t.Errorf("expected message_stop after ignored ping line, got: %q", downstream.String())
	}
}

func TestStream_FinishBeforeUsage_UsesPendingFinish(t *testing.T) {
	upstream := "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, "event: message_delta") {
		t.Errorf("expected message_delta (stop_reason) after finish-before-usage, got: %q", out)
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Errorf("expected message_stop, got: %q", out)
	}
	if strings.Count(out, "event: message_delta") != 1 {
		t.Errorf("expected exactly 1 message_delta, got: %q", out)
	}
}

func TestStream_FlushError_PropagatesErr(t *testing.T) {
	upstream := "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"
	var downstream bytes.Buffer
	flushErr := "simulated flush failure"
	flush := func() error { return errors.New(flushErr) }
	_, err := Stream(strings.NewReader(upstream), &downstream, flush)
	if err == nil {
		t.Fatal("expected error from flush")
	}
	if !strings.Contains(err.Error(), flushErr) {
		t.Errorf("expected flush error, got: %v", err)
	}
}

func TestRequest_ToolChoiceStringNone(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"tool_choice":"none",
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["tool_choice"] != "none" {
		t.Errorf("tool_choice: got %v, want none", got["tool_choice"])
	}
}

func TestRequest_ToolChoiceUnknownType(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"tool_choice":{"type":"weird"},
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["tool_choice"] != nil {
		t.Errorf("tool_choice: got %v, want nil", got["tool_choice"])
	}
}

func TestRequest_ToolChoiceToolNoName(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"tool_choice":{"type":"tool"},
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["tool_choice"] != nil {
		t.Errorf("tool_choice: got %v, want nil", got["tool_choice"])
	}
}

func TestRequest_SystemEmptyArray(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"system":[],
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

func TestRequest_StopMultiple(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"stop_sequences":["END","STOP"],
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	arr, ok := got["stop"].([]any)
	if !ok || len(arr) != 2 {
		t.Errorf("stop: got %v, want 2-element array", got["stop"])
	}
}

func TestRequest_NoMaxTokens(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["max_tokens"] != nil {
		t.Errorf("max_tokens: got %v, want nil", got["max_tokens"])
	}
}

func TestRequest_StreamNoStreamOptions(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["stream"] != nil {
		t.Errorf("stream: got %v, want nil", got["stream"])
	}
}

func TestRequest_Temperature(t *testing.T) {
	temp := 0.5
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"temperature":0.5,
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["temperature"] != temp {
		t.Errorf("temperature: got %v, want %v", got["temperature"], temp)
	}
}

func TestRequest_AssistantEmpty(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Errorf("assistant role: got %v", assistant["role"])
	}
	if assistant["content"] != nil {
		t.Errorf("assistant content: got %v, want nil", assistant["content"])
	}
	if _, ok := assistant["tool_calls"]; ok {
		t.Errorf("assistant should not have tool_calls key")
	}
}

func TestRequest_AssistantTextAndToolUse(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"assistant","content":[
				{"type":"text","text":"ok "},
				{"type":"tool_use","id":"t1","name":"do_thing","input":{}}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[0].(map[string]any)
	if assistant["content"] != "ok " {
		t.Errorf("assistant content: got %v, want 'ok '", assistant["content"])
	}
	tcs, ok := assistant["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("expected 1 tool_call, got %v", assistant["tool_calls"])
	}
}

func TestRequest_AssistantThinkingBlock(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"let me think..."},
				{"type":"text","text":"the answer"}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	if assistant["content"] != "the answer" {
		t.Errorf("assistant content: got %v, want 'the answer'", assistant["content"])
	}
	if assistant["reasoning_content"] != "let me think..." {
		t.Errorf(
			"reasoning_content: got %v, want 'let me think...'",
			assistant["reasoning_content"],
		)
	}
}

func TestRequest_AssistantThinkingOnlyBlock(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"just thinking"}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	if assistant["reasoning_content"] != "just thinking" {
		t.Errorf("reasoning_content: got %v, want 'just thinking'", assistant["reasoning_content"])
	}
	if _, ok := assistant["content"]; ok {
		t.Errorf("assistant should not have content when only thinking block")
	}
}

func TestRequest_AssistantReasoningContentTopLevel(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"the answer","reasoning_content":"the thinking"}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	if assistant["content"] != "the answer" {
		t.Errorf("assistant content: got %v, want 'the answer'", assistant["content"])
	}
	if assistant["reasoning_content"] != "the thinking" {
		t.Errorf("reasoning_content: got %v, want 'the thinking'", assistant["reasoning_content"])
	}
}

func TestStream_MultipleTextBlocks(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"first"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"second"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, "first") {
		t.Errorf("missing 'first': %q", out)
	}
	if !strings.Contains(out, "second") {
		t.Errorf("missing 'second': %q", out)
	}
}

func TestStream_CloseBeforeDone(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Errorf("unexpected error on clean EOF: %v", err)
	}
}

func TestStream_SwitchBlockTypes(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"text "},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"do","arguments":""}}]},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"type":"tool_use"`) {
		t.Errorf("missing tool_use content_block: %q", out)
	}
}

func TestRequest_ToolResultObjectContent(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":50,
		"messages":[
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":{"key":"value"}}]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	tool := msgs[0].(map[string]any)
	if tool["role"] != "tool" {
		t.Errorf("role: got %v, want tool", tool["role"])
	}
	if !strings.Contains(tool["content"].(string), `"key":"value"`) {
		t.Errorf("content: got %v", tool["content"])
	}
}

func TestRequest_SystemInvalidJSON(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"system":42,
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message (system invalid → dropped), got %d", len(msgs))
	}
}

func TestStream_NoOpenBlockFinish(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if strings.Contains(out, "content_block_stop") {
		t.Errorf("should not emit content_block_stop when no block was open: %q", out)
	}
}

func TestRequest_StreamWithUsage(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"stream":true,
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	streamOpts, ok := got["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("stream_options missing")
	}
	if streamOpts["include_usage"] != true {
		t.Errorf("include_usage: got %v", streamOpts["include_usage"])
	}
}

func TestRequest_ToolChoiceUnknownString(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"tool_choice":"some_unknown_string",
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["tool_choice"] != "some_unknown_string" {
		t.Errorf("unknown string choice should pass through, got %v", got["tool_choice"])
	}
}

func TestStream_DownstreamWriteError(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}

data: [DONE]

`
	w := &failingWriter{}
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), w, flush); err == nil {
		t.Fatal("expected error from failing writer")
	}
}

type failingWriter struct {
	count int
}

func (f *failingWriter) Write(p []byte) (int, error) {
	f.count++
	if f.count > 1 {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

func TestStream_MultilineData(t *testing.T) {
	upstream := "data: {\"a\":1}\n\ndata: {\"b\":2}\n\ndata: [DONE]\n\n"
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(downstream.String(), "event: message_stop") {
		t.Errorf("expected message_stop, got: %q", downstream.String())
	}
}

func TestStream_EOFOnPartialData(t *testing.T) {
	upstream := "data: {\"a\":1}\n"
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Errorf("unexpected error on partial EOF: %v", err)
	}
}

func TestStream_UnknownFinishReason(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"x"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"some_new_reason"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Errorf("unknown reason should default to end_turn, got: %q", out)
	}
}

func TestStream_TextThenToolThenFinish(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"do","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"type":"text"`) {
		t.Errorf("expected text block: %q", out)
	}
	if !strings.Contains(out, `"type":"tool_use"`) {
		t.Errorf("expected tool_use block: %q", out)
	}
}

func TestStream_FlushError(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return io.ErrShortWrite }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err == nil {
		t.Fatal("expected error when flush fails")
	}
}

func TestRequest_ToolResultWithText(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"sunny"}]}]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	tool := msgs[0].(map[string]any)
	if tool["role"] != "tool" {
		t.Errorf("role: got %v, want tool", tool["role"])
	}
}

func TestRequest_UserImageBlockPassesThrough(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"what is this?"},
				{"type":"image","source":{"type":"base64","data":"AAAA"}}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	if len(msgs) != 1 {
		t.Errorf("image block should be dropped, expected 1 message, got %d", len(msgs))
	}
}

func TestStream_ToolThenTextBlock(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"do","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"type":"tool_use"`) {
		t.Errorf("missing tool_use block: %q", out)
	}
	if !strings.Contains(out, `"type":"text"`) {
		t.Errorf("missing text block: %q", out)
	}
	if !strings.Contains(out, `"text":"done"`) {
		t.Errorf("missing text 'done' delta: %q", out)
	}
}

func TestStream_ToolCallSubsequentArgs(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"do_thing","arguments":"{\"x\":"}}]},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `\"x\":`) {
		t.Errorf("expected first arg fragment, got: %q", out)
	}
	if !strings.Contains(out, `1}`) {
		t.Errorf("expected second arg fragment, got: %q", out)
	}
}

func TestRequest_ToolChoiceObject(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"tool_choice":{"type":"tool","name":"get_weather"},
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	tc, ok := got["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice: got %T, want object", got["tool_choice"])
	}
	if tc["type"] != "function" {
		t.Errorf("tool_choice.type: got %v, want function", tc["type"])
	}
}

func TestRequest_ToolChoiceAny(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"tool_choice":{"type":"any"},
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["tool_choice"] != "required" {
		t.Errorf("tool_choice: got %v, want required", got["tool_choice"])
	}
}

func TestRequest_SystemAsBlocks(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"system":[{"type":"text","text":"Hello "},{"type":"text","text":"world"}],
		"messages":[{"role":"user","content":"hi"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	sys := msgs[0].(map[string]any)
	if sys["content"] != "Hello \nworld" {
		t.Errorf("system content: got %v, want %q", sys["content"], "Hello \nworld")
	}
}

func TestRequest_InvalidJSON(t *testing.T) {
	_, err := Request([]byte(`{not json`), "x", Opts{})
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestRequest_SystemRole(t *testing.T) {
	in := json.RawMessage(`{
		"model":"claude-opus-4-1",
		"max_tokens":10,
		"messages":[{"role":"system","content":"foo"}]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatalf("TranslateRequest with system role should succeed: %v", err)
	}
	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(parsed.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(parsed.Messages))
	}
	if parsed.Messages[0].Role != "system" {
		t.Errorf("expected role system, got %q", parsed.Messages[0].Role)
	}
	if parsed.Messages[0].Content != "foo" {
		t.Errorf("expected content foo, got %q", parsed.Messages[0].Content)
	}
}

func TestStream_ErrorMidStream(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: not-json

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err == nil {
		t.Fatal("expected error for malformed chunk")
	}
}

func TestStream_PendingFinishFlushesOnDone(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, "event: message_delta") {
		t.Errorf("expected message_delta in pending-finish path, got: %q", out)
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Errorf("expected message_stop, got: %q", out)
	}
}

func TestStream_SingleReasoningDelta(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning_content":"thinking hard"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"type":"thinking"`) {
		t.Errorf("expected thinking content_block, got: %q", out)
	}
	if !strings.Contains(out, `"type":"thinking_delta"`) {
		t.Errorf("expected thinking_delta event, got: %q", out)
	}
	if !strings.Contains(out, `"thinking":"thinking hard"`) {
		t.Errorf("expected thinking content in delta, got: %q", out)
	}
}

func TestStream_MultipleReasoningDeltas(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning_content":"first "},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning_content":"second"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	startCount := strings.Count(out, `"type":"thinking"`)
	if startCount != 1 {
		t.Errorf(
			"expected exactly 1 content_block_start(type=thinking), got %d in %q",
			startCount,
			out,
		)
	}
	deltaCount := strings.Count(out, `"type":"thinking_delta"`)
	if deltaCount != 2 {
		t.Errorf("expected 2 thinking_delta events, got %d in %q", deltaCount, out)
	}
}

func TestStream_ReasoningThenTextTransition(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning_content":"thinking"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"type":"thinking"`) {
		t.Errorf("expected thinking block, got: %q", out)
	}
	if !strings.Contains(out, `"type":"text"`) {
		t.Errorf("expected text block, got: %q", out)
	}
	// content_block_stop should appear at least twice (once for thinking, once for text)
	stopCount := strings.Count(out, "content_block_stop")
	if stopCount < 2 {
		t.Errorf(
			"expected at least 2 content_block_stop events (thinking close + text close), got %d in %q",
			stopCount,
			out,
		)
	}
	// Regression: verify block indexes are consistent (start index matches stop index)
	if !strings.Contains(out, `"index":0,"type":"content_block_stop"`) {
		t.Errorf("expected thinking block stop at index 0, got: %q", out)
	}
	if !strings.Contains(out, `"index":1,"type":"content_block_stop"`) {
		t.Errorf("expected text block stop at index 1, got: %q", out)
	}
}

func TestStream_TextThenReasoningTransition(t *testing.T) {
	upstream := `data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning_content":"thought"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	var downstream bytes.Buffer
	flush := func() error { return nil }
	if _, err := Stream(strings.NewReader(upstream), &downstream, flush); err != nil {
		t.Fatal(err)
	}
	out := downstream.String()
	if !strings.Contains(out, `"type":"text"`) {
		t.Errorf("expected text block, got: %q", out)
	}
	if !strings.Contains(out, `"type":"thinking"`) {
		t.Errorf("expected thinking block, got: %q", out)
	}
	stopCount := strings.Count(out, "content_block_stop")
	if stopCount < 2 {
		t.Errorf(
			"expected at least 2 content_block_stop events (text close + thinking close), got %d in %q",
			stopCount,
			out,
		)
	}
}

func TestRequest_NoStreamUsageOmitsStreamOptions(t *testing.T) {
	in := []byte(
		`{"model":"x","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	)
	out, err := Request(in, "x", Opts{NoStreamUsage: true})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["stream_options"]; ok {
		t.Errorf(
			"expected stream_options absent when NoStreamUsage=true, got %v",
			got["stream_options"],
		)
	}
}

func TestRequest_NoStreamUsageFalseIncludesStreamOptions(t *testing.T) {
	in := []byte(
		`{"model":"x","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	)
	out, err := Request(in, "x", Opts{NoStreamUsage: false})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	streamOpts, ok := got["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("stream_options missing")
	}
	if streamOpts["include_usage"] != true {
		t.Errorf("include_usage: got %v, want true", streamOpts["include_usage"])
	}
}

func TestRequest_ReasoningContentOnToolCallWithThinking(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"let me reason"},
				{"type":"tool_use","id":"t1","name":"do","input":{}}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	rc, ok := assistant["reasoning_content"]
	if !ok {
		t.Fatal("reasoning_content missing on tool_call message with thinking")
	}
	if rc != "let me reason" {
		t.Errorf("reasoning_content: got %v, want 'let me reason'", rc)
	}
}

func TestRequest_PlaceholderInjectedOnToolCallWithoutThinking(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"reasoning here"},
				{"type":"text","text":"answer"}
			]},
			{"role":"user","content":"use tool"},
			{"role":"assistant","content":[
				{"type":"tool_use","id":"t1","name":"do","input":{}}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	// msgs[2] is the second assistant (tool_call without thinking)
	assistant := msgs[3].(map[string]any)
	rc, ok := assistant["reasoning_content"]
	if !ok {
		t.Fatal("reasoning_content missing — placeholder should be injected")
	}
	if rc != " " {
		t.Errorf("reasoning_content: got %q, want single space", rc)
	}
}

func TestRequest_NoReasoningWhenNoThinkingInConversation(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"tool_use","id":"t1","name":"do","input":{}}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	if _, ok := assistant["reasoning_content"]; ok {
		t.Error("reasoning_content should NOT be present when no thinking in conversation")
	}
}

func TestRequest_MultipleThinkingBlocksConcatenated(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"hello"},
				{"type":"thinking","thinking":"world"},
				{"type":"text","text":"done"}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	rc, ok := assistant["reasoning_content"]
	if !ok {
		t.Fatal("reasoning_content missing")
	}
	if rc != "hello\nworld" {
		t.Errorf("reasoning_content: got %q, want %q", rc, "hello\nworld")
	}
}

func TestRequest_NoInjectionOnAssistantWithoutToolCalls(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"deep thought"},
				{"type":"tool_use","id":"t1","name":"do","input":{}}
			]},
			{"role":"user","content":"thanks"},
			{"role":"assistant","content":[
				{"type":"text","text":"you're welcome"}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	// Last assistant has no tool_calls — should NOT get reasoning injected
	lastAssistant := msgs[3].(map[string]any)
	if _, ok := lastAssistant["reasoning_content"]; ok {
		t.Error("reasoning_content should NOT be injected on assistant without tool_calls")
	}
}

func TestRequest_EmptyThinkingBlockProducesPlaceholder(t *testing.T) {
	in := []byte(`{
		"model":"x",
		"max_tokens":10,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":""},
				{"type":"text","text":"answer"}
			]}
		]
	}`)
	out, err := Request(in, "x", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	msgs := got["messages"].([]any)
	assistant := msgs[1].(map[string]any)
	rc, ok := assistant["reasoning_content"]
	if !ok {
		t.Fatal("reasoning_content missing — empty thinking should still produce field")
	}
	if rc != " " {
		t.Errorf("reasoning_content: got %q, want single space placeholder", rc)
	}
}
