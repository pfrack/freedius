// DO NOT log request or response bodies in this file.
// freedius NFR-Privacy (prd.md): no request or response payload is logged
// to disk or transmitted beyond the target provider. Metadata (model name,
// provider, status code) is acceptable; message content, tool arguments,
// tool results, and API responses are not.

package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
)

type TranslateOpts struct {
	NoStreamUsage bool
}

// reasoningCache stores the last observed reasoning_content per model name.
// Key: model string, Value: accumulated reasoning text.
var reasoningCache sync.Map

// CacheReasoning stores reasoning content for a model.
func CacheReasoning(model, reasoning string) {
	reasoningCache.Store(model, reasoning)
}

// LoadCachedReasoning retrieves cached reasoning content for a model.
func LoadCachedReasoning(model string) string {
	v, ok := reasoningCache.Load(model)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func InjectReasoningIntoOpenAI(body []byte, model string) ([]byte, error) {
	reasoning := LoadCachedReasoning(model)
	if reasoning == "" {
		return body, nil
	}
	var req openAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	for i, msg := range req.Messages {
		if msg.Role == "assistant" && msg.ReasoningContent == "" {
			req.Messages[i].ReasoningContent = reasoning
		}
	}
	return json.Marshal(req)
}

func TranslateRequest(anthropicBody []byte, targetModel string, opts TranslateOpts) ([]byte, error) {
	var req anthropicMessage
	if err := json.Unmarshal(anthropicBody, &req); err != nil {
		return nil, fmt.Errorf("translate: parse anthropic body: %w", err)
	}
	if targetModel != "" {
		req.Model = targetModel
	}

	out := openAIRequest{
		Model:       req.Model,
		MaxTokens:   intPtrOrNil(req.MaxTokens),
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		Tools:       convertTools(req.Tools),
		ToolChoice:  convertToolChoice(req.ToolChoice),
		Stop:        convertStop(req.StopSequences),
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
		return nil
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
	return out, nil
}

func extractSystemText(system any) string {
	if system == nil {
		return ""
	}
	switch s := system.(type) {
	case string:
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

	if m.Role == "user" {
		if str, ok := m.Content.(string); ok {
			return []openAIMessage{{Role: "user", Content: str}}, nil
		}
		var blocks []map[string]any
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return nil, fmt.Errorf("translate: parse user content: %w", err)
		}
		var out []openAIMessage
		for _, b := range blocks {
			btype, _ := b["type"].(string)
			switch btype {
			case "text":
				t, _ := b["text"].(string)
				if t != "" {
					out = append(out, openAIMessage{Role: "user", Content: t})
				}
			case "tool_result":
				content := stringifyContent(b["content"])
				toolID, _ := b["tool_use_id"].(string)
				out = append(out, openAIMessage{
					Role:       "tool",
					Content:    content,
					ToolCallID: toolID,
				})
			}
		}
		return out, nil
	}

	if m.Role == "assistant" {
		om := openAIMessage{Role: "assistant"}
		var textParts []string
		var toolCalls []openAIToolCall

		if str, ok := m.Content.(string); ok && str != "" {
			textParts = append(textParts, str)
		} else {
			var blocks []map[string]any
			if err := json.Unmarshal(raw, &blocks); err != nil {
				return nil, fmt.Errorf("translate: parse assistant content: %w", err)
			}
			for _, b := range blocks {
				btype, _ := b["type"].(string)
				switch btype {
				case "text":
					t, _ := b["text"].(string)
					if t != "" {
						textParts = append(textParts, t)
					}
				case "thinking":
					thinking, _ := b["thinking"].(string)
					if thinking != "" {
						om.ReasoningContent = thinking
					}
				case "tool_use":
					id, _ := b["id"].(string)
					name, _ := b["name"].(string)
					inputRaw, _ := json.Marshal(b["input"])
					tc := openAIToolCall{
						Index: 0,
						ID:    id,
						Type:  "function",
						Function: openAIToolCallFunc{
							Name:      name,
							Arguments: string(inputRaw),
						},
					}
					toolCalls = append(toolCalls, tc)
				}
			}
		}

		if len(textParts) > 0 {
			om.Content = strings.Join(textParts, "")
		}
		if len(toolCalls) > 0 {
			om.ToolCalls = toolCalls
		}
		return []openAIMessage{om}, nil
	}

	if m.Role == "system" {
		content := ""
		if str, ok := m.Content.(string); ok {
			content = str
		} else {
			var blocks []map[string]any
			if err := json.Unmarshal(raw, &blocks); err == nil {
				var parts []string
				for _, b := range blocks {
					if btype, _ := b["type"].(string); btype == "text" {
						if t, _ := b["text"].(string); t != "" {
							parts = append(parts, t)
						}
					}
				}
				content = strings.Join(parts, "\n")
			}
		}
		return []openAIMessage{{Role: "system", Content: content}}, nil
	}

	return nil, fmt.Errorf("translate: unsupported message role %q", m.Role)
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

func TranslateStream(upstream io.Reader, downstream io.Writer, flush func() error) (string, error) {
	br := bufio.NewReaderSize(upstream, 64*1024)
	em := newAnthropicEmitter()
	for {
		chunk, err := readSSEEvent(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return em.thinkingBuffer, nil
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
	messageID      string
	model          string
	blockIndex     int
	openBlock      string
	toolToBlock    map[int]int
	toolNames      map[int]string
	toolIDs        map[int]string
	inputTokens    int
	outputTokens   int
	sawUsage       bool
	roleSent       bool
	finished       bool
	pendingFinish  string
	thinkingBuffer string
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
			e.thinkingBuffer += ch.Delta.ReasoningContent
		}
		if ch.Delta.Content != "" {
			content := ch.Delta.Content
			if e.thinkingBuffer != "" {
				content = e.thinkingBuffer + "\n\n" + content
				e.thinkingBuffer = ""
			}
			ev, err := e.emitText(content)
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
		}
		if len(ch.Delta.ToolCalls) > 0 && e.thinkingBuffer != "" {
			ev, err := e.emitText(e.thinkingBuffer)
			if err != nil {
				return nil, err
			}
			events = append(events, ev...)
			e.thinkingBuffer = ""
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
			if e.thinkingBuffer != "" {
				ev, err := e.emitText(e.thinkingBuffer)
				if err != nil {
					return nil, err
				}
				events = append(events, ev...)
				e.thinkingBuffer = ""
			}
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
	if e.thinkingBuffer != "" {
		ev, err := e.emitText(e.thinkingBuffer)
		if err != nil {
			return nil, err
		}
		events = append(events, ev...)
		e.thinkingBuffer = ""
	}
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
			"id":      e.messageID,
			"type":    "message",
			"role":    "assistant",
			"model":   e.model,
			"content": []any{},
			"stop_reason": nil,
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
		if tc.Function.Arguments == "" {
			return startEv, nil
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
		return append(startEv, deltaEv...), nil
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
