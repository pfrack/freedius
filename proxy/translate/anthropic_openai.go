// DO NOT log request or response bodies in this file.
// freedius NFR-Privacy (prd.md): no request or response payload is logged
// to disk or transmitted beyond the target provider. Metadata (model name,
// provider, status code) is acceptable; message content, tool arguments,
// tool results, and API responses are not.

// Package translate converts between Anthropic Messages API and OpenAI
// Chat Completions API request/response shapes. Translation is the only
// responsibility of this package: the HTTP plumbing lives in proxy.
package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
)

// Opts tunes the request translation. Currently used to suppress the
// OpenAI stream_options.include_usage field for providers that don't support it.
type Opts struct {
	NoStreamUsage bool
}

// Request converts an Anthropic-format request body into the
// equivalent OpenAI Chat Completions request body, rewriting the model name
// to targetModel and honoring opts.
func Request(
	anthropicBody []byte,
	targetModel string,
	opts Opts,
) ([]byte, error) {
	var req anthropicMessage
	if err := json.Unmarshal(anthropicBody, &req); err != nil {
		return nil, fmt.Errorf("translate: parse anthropic body: %w", err)
	}
	if targetModel != "" {
		req.Model = targetModel
	}

	out := openAIRequest{
		Model:         req.Model,
		MaxTokens:     intPtrOrNil(req.MaxTokens),
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		Stream:        req.Stream,
		Tools:         convertTools(req.Tools),
		ToolChoice:    convertToolChoice(req.ToolChoice),
		Stop:          convertStop(req.StopSequences),
		StreamOptions: nil,
	}

	if req.Stream && !opts.NoStreamUsage {
		out.StreamOptions = &openAIStreamOpts{IncludeUsage: true}
	}

	messages, err := convertMessages(req.System, req.Messages)
	if err != nil {
		return nil, fmt.Errorf("translate: convert messages: %w", err)
	}
	out.Messages = messages

	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("translate: marshal openai body: %w", err)
	}
	return body, nil
}

func intPtrOrNil(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func convertStop(stop []string) any {
	if len(stop) == 0 {
		return nil
	}
	if len(stop) == 1 {
		return stop[0]
	}
	return stop
}

func convertTools(in []anthropicTool) []openAITool {
	if len(in) == 0 {
		return nil
	}
	out := make([]openAITool, 0, len(in))
	for _, t := range in {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

func convertToolChoice(tc any) any {
	if tc == nil {
		return nil
	}
	if s, ok := tc.(string); ok {
		switch s {
		case "auto", "any":
			return s
		case "none":
			return "none"
		default:
			return s
		}
	}
	raw, err := json.Marshal(tc)
	if err != nil {
		return tc
	}
	var probe struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	switch probe.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		if probe.Name != "" {
			return map[string]any{
				"type":     "function",
				"function": map[string]string{"name": probe.Name},
			}
		}
		return nil
	default:
		return nil
	}
}

func strPtr(s string) *string { return &s }

func convertMessages(system any, msgs []anthropicMsgItem) ([]openAIMessage, error) {
	var out []openAIMessage

	if sysText := extractSystemText(system); sysText != "" {
		out = append(out, openAIMessage{Role: "system", Content: sysText})
	}

	for _, m := range msgs {
		converted, err := convertOneMessage(m)
		if err != nil {
			return nil, err
		}
		out = append(out, converted...)
	}

	// Post-pass: if any assistant message has reasoning, ensure all assistant
	// messages with tool_calls also have it (DeepSeek/Kimi require non-empty
	// reasoning_content on tool_call messages once thinking mode is active).
	hasReasoning := false
	for _, m := range out {
		if m.Role == "assistant" && m.ReasoningContent != nil {
			hasReasoning = true
			break
		}
	}
	if hasReasoning {
		for i := range out {
			if out[i].Role == "assistant" && len(out[i].ToolCalls) > 0 &&
				out[i].ReasoningContent == nil {
				out[i].ReasoningContent = strPtr(" ")
			}
		}
	}

	return out, nil
}

func extractSystemText(system any) string {
	if system == nil {
		return ""
	}
	if s, ok := system.(string); ok {
		return s
	}
	raw, err := json.Marshal(system)
	if err != nil {
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func convertOneMessage(m anthropicMsgItem) ([]openAIMessage, error) {
	raw, err := json.Marshal(m.Content)
	if err != nil {
		return nil, err
	}
	switch m.Role {
	case "user":
		return convertUserMessage(raw, m.Content)
	case "assistant":
		return convertAssistantMessage(raw, m.Content, m.ReasoningContent)
	case "system":
		return convertSystemMessage(raw, m.Content)
	default:
		return nil, fmt.Errorf("translate: unsupported message role %q", m.Role)
	}
}

func convertUserMessage(raw []byte, content any) ([]openAIMessage, error) {
	if str, ok := content.(string); ok {
		return []openAIMessage{{Role: "user", Content: str}}, nil
	}
	blocks, err := decodeContentBlocks(raw)
	if err != nil {
		return nil, err
	}
	var out []openAIMessage
	for _, b := range blocks {
		if msg, ok := userBlockToMessage(b); ok {
			out = append(out, msg)
		}
	}
	return out, nil
}

func userBlockToMessage(b map[string]any) (openAIMessage, bool) {
	switch b["type"].(string) {
	case "text":
		t, _ := b["text"].(string)
		if t == "" {
			return openAIMessage{}, false
		}
		return openAIMessage{Role: "user", Content: t}, true
	case "tool_result":
		toolID, _ := b["tool_use_id"].(string)
		return openAIMessage{
			Role:       "tool",
			Content:    stringifyContent(b["content"]),
			ToolCallID: toolID,
		}, true
	}
	return openAIMessage{}, false
}

func convertAssistantMessage(raw []byte, content any, topLevelReasoning string) ([]openAIMessage, error) {
	om := openAIMessage{Role: "assistant"}

	if str, ok := content.(string); ok && str != "" {
		om.Content = str
		om.ReasoningContent = assistantReasoning(topLevelReasoning, nil)
		return []openAIMessage{om}, nil
	}

	blocks, err := decodeContentBlocks(raw)
	if err != nil {
		return nil, err
	}
	var textParts, thinkingParts []string
	var toolCalls []openAIToolCall
	for _, b := range blocks {
		applyAssistantBlock(b, &textParts, &thinkingParts, &toolCalls)
	}

	om.ReasoningContent = assistantReasoning(topLevelReasoning, thinkingParts)
	if len(textParts) > 0 {
		om.Content = strings.Join(textParts, "")
	}
	if len(toolCalls) > 0 {
		om.ToolCalls = toolCalls
	}
	return []openAIMessage{om}, nil
}

func applyAssistantBlock(
	b map[string]any,
	textParts *[]string,
	thinkingParts *[]string,
	toolCalls *[]openAIToolCall,
) {
	switch b["type"].(string) {
	case "text":
		if t, _ := b["text"].(string); t != "" {
			*textParts = append(*textParts, t)
		}
	case "thinking":
		thinking, _ := b["thinking"].(string)
		*thinkingParts = append(*thinkingParts, thinking)
	case "tool_use":
		*toolCalls = append(*toolCalls, toolUseToToolCall(b))
	}
}

func assistantReasoning(topLevel string, blocks []string) *string {
	if len(blocks) > 0 {
		joined := strings.Join(blocks, "\n")
		if strings.TrimSpace(joined) == "" {
			joined = " "
		}
		return strPtr(joined)
	}
	if topLevel != "" {
		return strPtr(topLevel)
	}
	return nil
}

func toolUseToToolCall(b map[string]any) openAIToolCall {
	id, _ := b["id"].(string)
	name, _ := b["name"].(string)
	input := b["input"]
	if input == nil {
		input = map[string]any{}
	}
	inputRaw, _ := json.Marshal(input)
	return openAIToolCall{
		Index: 0,
		ID:    id,
		Type:  "function",
		Function: openAIToolCallFunc{
			Name:      name,
			Arguments: string(inputRaw),
		},
	}
}

func convertSystemMessage(raw []byte, content any) ([]openAIMessage, error) {
	if str, ok := content.(string); ok {
		return []openAIMessage{{Role: "system", Content: str}}, nil
	}
	blocks, err := decodeContentBlocks(raw)
	if err != nil {
		return nil, err
	}
	var parts []string
	for _, b := range blocks {
		if b["type"].(string) == "text" {
			if t, _ := b["text"].(string); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return []openAIMessage{{Role: "system", Content: strings.Join(parts, "\n")}}, nil
}

func decodeContentBlocks(raw []byte) ([]map[string]any, error) {
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("translate: parse message content: %w", err)
	}
	return blocks, nil
}

func stringifyContent(c any) string {
	if s, ok := c.(string); ok {
		return s
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(raw)
}

// Stream reads OpenAI-format SSE chunks from upstream, translates
// them into Anthropic-format SSE events, writes them to downstream, and
// flushes after each event using the provided flush callback. The returned
// string is the assistant text used as input_reasoning (if any) and a
// non-nil error indicates the stream ended abnormally.
func Stream(upstream io.Reader, downstream io.Writer, flush func() error) (string, error) {
	br := bufio.NewReaderSize(upstream, 64*1024)
	em := newAnthropicEmitter()
	for {
		chunk, err := readSSEEvent(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", nil
			}
			return "", err
		}
		events, err := em.consume(chunk)
		if err != nil {
			return "", err
		}
		for _, ev := range events {
			if _, err := downstream.Write(ev); err != nil {
				return "", err
			}
			if err := flush(); err != nil {
				return "", err
			}
		}
	}
}

func readSSEEvent(br *bufio.Reader) ([]byte, error) {
	var dataLine []byte
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			if bytes.HasPrefix(trimmed, []byte("data:")) {
				value := bytes.TrimPrefix(trimmed, []byte("data:"))
				value = bytes.TrimLeft(value, " ")
				if dataLine == nil {
					dataLine = append([]byte{}, value...)
				} else {
					dataLine = append(dataLine, '\n')
					dataLine = append(dataLine, value...)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if dataLine == nil {
					return nil, io.EOF
				}
				return dataLine, nil
			}
			return nil, err
		}
		if len(bytes.TrimRight(line, "\r\n")) == 0 && dataLine != nil {
			return dataLine, nil
		}
	}
}

type emitter struct {
	messageID     string
	model         string
	blockIndex    int
	openBlock     string
	toolToBlock   map[int]int
	toolNames     map[int]string
	toolIDs       map[int]string
	inputTokens   int
	outputTokens  int
	sawUsage      bool
	roleSent      bool
	finished      bool
	pendingFinish string
}

func newAnthropicEmitter() *emitter {
	return &emitter{
		messageID:   "msg_" + randomID(),
		toolToBlock: map[int]int{},
		toolNames:   map[int]string{},
		toolIDs:     map[int]string{},
	}
}

func (e *emitter) consume(payload []byte) ([][]byte, error) {
	trimmed := bytes.TrimSpace(payload)
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		if e.finished {
			return nil, nil
		}
		e.finished = true
		return e.flushPending()
	}

	var chunk openAIChunk
	if err := json.Unmarshal(trimmed, &chunk); err != nil {
		return nil, fmt.Errorf("translate: parse openai chunk: %w", err)
	}

	if chunk.Model != "" {
		e.model = chunk.Model
	}
	if chunk.Usage != nil {
		e.outputTokens = chunk.Usage.CompletionTokens
		e.inputTokens = chunk.Usage.PromptTokens
		e.sawUsage = true
	}

	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	var events [][]byte
	for _, ch := range chunk.Choices {
		if ch.Delta.Role != "" && !e.roleSent {
			ev, err := e.emitMessageStart()
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
		}
		if ch.Delta.ReasoningContent != "" {
			ev, err := e.emitThinkingDelta(ch.Delta.ReasoningContent)
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
		}
		if ch.Delta.Content != "" {
			ev, err := e.emitText(ch.Delta.Content)
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
		}
		for _, tc := range ch.Delta.ToolCalls {
			ev, err := e.emitToolCall(tc)
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
		}
		if ch.FinishReason != nil {
			e.pendingFinish = *ch.FinishReason
			if e.sawUsage {
				ev, err := e.emitFinish(*ch.FinishReason)
				if err != nil {
					return nil, err
				}
				events = append(events, ev...)
			}
		}
	}
	return events, nil
}

func (e *emitter) flushPending() ([][]byte, error) {
	var events [][]byte
	if e.pendingFinish != "" {
		ev, err := e.emitFinish(e.pendingFinish)
		if err != nil {
			return nil, err
		}
		events = append(events, ev...)
	}
	ev, err := e.emitMessageStop()
	if err != nil {
		return nil, err
	}
	events = append(events, ev...)
	return events, nil
}

func (e *emitter) emitMessageStart() ([][]byte, error) {
	e.roleSent = true
	payload := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            e.messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         e.model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]int{
				"input_tokens":  e.inputTokens,
				"output_tokens": e.outputTokens,
			},
		},
	}
	return e.emit("message_start", payload)
}

func (e *emitter) emitText(text string) ([][]byte, error) {
	var events [][]byte
	if e.openBlock != "text" {
		if e.openBlock != "" {
			ev, err := e.emitBlockStop()
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
		}
		start := map[string]any{
			"type":  "content_block_start",
			"index": e.blockIndex,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		}
		ev, err := e.emit("content_block_start", start)
		if err != nil {
			return nil, err
		}
		events = append(events, ev...)
		e.openBlock = "text"
	}
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": e.blockIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	}
	ev, err := e.emit("content_block_delta", delta)
	if err != nil {
		return nil, err
	}
	events = append(events, ev...)
	return events, nil
}

func (e *emitter) emitThinkingDelta(thinking string) ([][]byte, error) {
	var events [][]byte
	if e.openBlock != "thinking" {
		if e.openBlock != "" {
			ev, err := e.emitBlockStop()
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
		}
		start := map[string]any{
			"type":  "content_block_start",
			"index": e.blockIndex,
			"content_block": map[string]any{
				"type":     "thinking",
				"thinking": "",
			},
		}
		ev, err := e.emit("content_block_start", start)
		if err != nil {
			return nil, err
		}
		events = append(events, ev...)
		e.blockIndex++
		e.openBlock = "thinking"
	}
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": e.blockIndex - 1,
		"delta": map[string]any{
			"type":     "thinking_delta",
			"thinking": thinking,
		},
	}
	ev, err := e.emit("content_block_delta", delta)
	if err != nil {
		return nil, err
	}
	events = append(events, ev...)
	return events, nil
}

func (e *emitter) emitToolCall(tc openAIToolCall) ([][]byte, error) {
	if _, ok := e.toolToBlock[tc.Index]; !ok {
		var events [][]byte
		if e.openBlock != "" {
			ev, err := e.emitBlockStop()
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
		}
		e.toolToBlock[tc.Index] = e.blockIndex
		if tc.ID != "" {
			e.toolIDs[tc.Index] = tc.ID
		}
		if tc.Function.Name != "" {
			e.toolNames[tc.Index] = tc.Function.Name
		}
		start := map[string]any{
			"type":  "content_block_start",
			"index": e.blockIndex,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    e.toolIDs[tc.Index],
				"name":  e.toolNames[tc.Index],
				"input": map[string]any{},
			},
		}
		e.blockIndex++
		e.openBlock = "tool"
		startEv, err := e.emit("content_block_start", start)
		if err != nil {
			return nil, err
		}
		events = append(events, startEv...)
		if tc.Function.Arguments == "" {
			return events, nil
		}
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.toolToBlock[tc.Index],
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": tc.Function.Arguments,
			},
		}
		deltaEv, err := e.emit("content_block_delta", delta)
		if err != nil {
			return nil, err
		}
		events = append(events, deltaEv...)
		return events, nil
	}
	if tc.Function.Arguments == "" {
		return nil, nil
	}
	idx := e.toolToBlock[tc.Index]
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": tc.Function.Arguments,
		},
	}
	return e.emit("content_block_delta", delta)
}

func (e *emitter) emitFinish(reason string) ([][]byte, error) {
	var events [][]byte
	if e.openBlock != "" {
		ev, err := e.emitBlockStop()
		if err != nil {
			return nil, err
		}
		events = append(events, ev...)
	}
	stopReason := mapFinishReason(reason)
	md := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]int{
			"output_tokens": e.outputTokens,
		},
	}
	ev, err := e.emit("message_delta", md)
	if err != nil {
		return nil, err
	}
	events = append(events, ev...)
	return events, nil
}

func (e *emitter) emitBlockStop() ([][]byte, error) {
	idx := e.blockIndex
	if e.openBlock == "text" || e.openBlock == "tool" || e.openBlock == "thinking" {
		idx--
	}
	payload := map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	}
	e.openBlock = ""
	return e.emit("content_block_stop", payload)
}

func (e *emitter) emitMessageStop() ([][]byte, error) {
	payload := map[string]any{"type": "message_stop"}
	return e.emit("message_stop", payload)
}

func (e *emitter) emit(eventType string, payload any) ([][]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	line := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, raw)
	return [][]byte{[]byte(line)}, nil
}

func mapFinishReason(r string) string {
	switch r {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}

var idCounter uint64

func randomID() string {
	id := atomic.AddUint64(&idCounter, 1)
	return fmt.Sprintf("translate-%d", id)
}
