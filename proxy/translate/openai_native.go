// DO NOT log request or response bodies in this file.
// freedius NFR-Privacy (prd.md): no request or response payload is logged
// to disk or transmitted beyond the target provider. See the note in
// anthropic_openai.go for the full rationale.

package translate

// This file holds the two directions that are not a full Anthropic<->OpenAI
// protocol translation:
//
//   - Passthrough: an OpenAI-format client (POST /v1/chat/completions) posts a
//     body that is already OpenAI shaped. Forwarding it untranslated keeps
//     fields this package does not model (max_completion_tokens,
//     response_format, parallel_tool_calls, ...) intact.
//   - Response/Message and Completion: a client that asked for a non-streaming
//     reply gets a single JSON document — Anthropic Message or OpenAI
//     chat.completion, matching the wire format it speaks — not an event
//     stream.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Passthrough rewrites an OpenAI Chat Completions request for the upstream
// without translating anything else: the model is remapped to targetModel and
// max_tokens (or max_completion_tokens) is clamped to opts.MaxTokensCap when
// set. Every other field the client sent survives untouched, so provider-
// specific extensions are not silently dropped.
//
// Bodies that are not a JSON object are rejected. Decoding uses
// json.Decoder.UseNumber so integer fields are re-emitted verbatim instead of
// being rounded through float64.
func Passthrough(body []byte, targetModel string, opts Opts) ([]byte, error) {
	obj, err := decodeJSONObject(body)
	if err != nil {
		return nil, fmt.Errorf("translate: parse openai body: %w", err)
	}
	if targetModel != "" {
		obj["model"] = targetModel
	}
	if opts.MaxTokensCap > 0 {
		clampJSONInt(obj, "max_tokens", opts.MaxTokensCap)
		clampJSONInt(obj, "max_completion_tokens", opts.MaxTokensCap)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("translate: marshal openai body: %w", err)
	}
	return out, nil
}

// Response converts a non-streaming OpenAI Chat Completions response into the
// Anthropic Message shape that a `stream:false` POST /v1/messages caller
// expects. modelOverride, when non-empty, replaces the upstream model name so
// the client sees the model it asked for.
//
// The message id is generated rather than taken from the upstream response:
// these are synthesized Anthropic messages, and Anthropic ids are msg_-prefixed,
// not the upstream's chatcmpl- form. It matches the id the streaming path emits
// for the same conversation.
func Response(body []byte, modelOverride string) ([]byte, error) {
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content          string           `json:"content"`
				ReasoningContent string           `json:"reasoning_content"`
				ToolCalls        []openAIToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *openAIUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("translate: parse openai response: %w", err)
	}

	acc := &messageAccumulator{model: resp.Model}
	if resp.Usage != nil {
		acc.setUsage(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]
		if ch.Message.ReasoningContent != "" {
			acc.addThinking(ch.Message.ReasoningContent)
		}
		if ch.Message.Content != "" {
			acc.addText(ch.Message.Content)
		}
		for _, tc := range ch.Message.ToolCalls {
			acc.addToolArgs(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		acc.stopReason = mapFinishReason(ch.FinishReason)
	}

	id := "msg_" + randomID()
	return acc.message(id, modelOverride)
}

// Message converts an OpenAI event stream into a single Anthropic Message JSON
// document. It covers the case where the client asked for a non-streaming
// reply but the upstream answered with SSE anyway — the SSE is translated and
// then folded into one Message instead of being handed to the client as an
// event stream it cannot parse.
func Message(upstream io.Reader, modelOverride string) ([]byte, error) {
	em := newAnthropicEmitter(modelOverride)
	em.acc = &messageAccumulator{}
	if err := streamInto(em, upstream, io.Discard, func() error { return nil }); err != nil {
		return nil, err
	}
	return em.acc.message(em.messageID, modelOverride)
}

// Completion converts an OpenAI event stream into a single chat.completion
// document. It is the OpenAI-wire-format counterpart of Message: it covers a
// client that asked for a non-streaming reply while the upstream streamed
// anyway, so the client gets the JSON it asked for rather than an event stream.
func Completion(upstream io.Reader, modelOverride string) ([]byte, error) {
	acc := &completionAccumulator{}
	br := bufio.NewReaderSize(upstream, 64*1024)
	for {
		payload, err := readSSEEvent(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return acc.completion(modelOverride)
			}
			return nil, err
		}
		trimmed := bytes.TrimSpace(payload)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("[DONE]")) {
			continue
		}
		var chunk openAIChunk
		if err := json.Unmarshal(trimmed, &chunk); err != nil {
			return nil, fmt.Errorf("translate: parse openai chunk: %w", err)
		}
		acc.consume(chunk)
	}
}

// completionAccumulator folds OpenAI chunks into one chat.completion. The
// upstream's "object" is deliberately not inherited: a chunk reports
// chat.completion.chunk, while the folded document must report the
// non-streaming chat.completion.
type completionAccumulator struct {
	id        string
	created   int64
	model     string
	role      string
	text      strings.Builder
	reasoning strings.Builder
	tools     []openAIToolCall
	finish    string
	usage     *openAIUsage
	seen      bool
}

func (a *completionAccumulator) consume(chunk openAIChunk) {
	a.seen = true
	if a.id == "" {
		a.id = chunk.ID
	}
	if a.created == 0 {
		a.created = chunk.Created
	}
	if chunk.Model != "" {
		a.model = chunk.Model
	}
	if chunk.Usage != nil {
		usage := *chunk.Usage
		a.usage = &usage
	}
	for _, ch := range chunk.Choices {
		if ch.Delta.Role != "" {
			a.role = ch.Delta.Role
		}
		if ch.Delta.ReasoningContent != "" {
			a.reasoning.WriteString(ch.Delta.ReasoningContent)
		}
		if ch.Delta.Content != "" {
			a.text.WriteString(ch.Delta.Content)
		}
		a.addToolDeltas(ch.Delta.ToolCalls)
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			a.finish = *ch.FinishReason
		}
	}
}

// addToolDeltas merges streamed tool-call fragments, which arrive split across
// chunks and identified by their choice index rather than by position.
func (a *completionAccumulator) addToolDeltas(deltas []openAIToolCall) {
	for _, d := range deltas {
		merged := false
		for i := range a.tools {
			if a.tools[i].Index != d.Index {
				continue
			}
			if d.ID != "" {
				a.tools[i].ID = d.ID
			}
			if d.Type != "" {
				a.tools[i].Type = d.Type
			}
			if d.Function.Name != "" {
				a.tools[i].Function.Name = d.Function.Name
			}
			a.tools[i].Function.Arguments += d.Function.Arguments
			merged = true
			break
		}
		if !merged {
			a.tools = append(a.tools, d)
		}
	}
}

func (a *completionAccumulator) completion(modelOverride string) ([]byte, error) {
	model := a.model
	if modelOverride != "" {
		model = modelOverride
	}
	id := a.id
	if id == "" {
		id = "chatcmpl-" + randomID()
	}
	object := "chat.completion"
	created := a.created
	if created == 0 {
		created = time.Now().Unix()
	}
	role := a.role
	if role == "" {
		role = "assistant"
	}
	message := map[string]any{"role": role, "content": a.text.String()}
	if a.reasoning.Len() > 0 {
		message["reasoning_content"] = a.reasoning.String()
	}
	if len(a.tools) > 0 {
		message["tool_calls"] = a.tools
	}
	finish := a.finish
	if finish == "" {
		finish = "stop"
	}
	payload := map[string]any{
		"id":      id,
		"object":  object,
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
	}
	if a.usage != nil {
		payload["usage"] = a.usage
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("translate: marshal openai completion: %w", err)
	}
	return out, nil
}

// messageAccumulator folds translated Anthropic events into the content blocks,
// stop reason and usage of a single non-streaming Message.
type messageAccumulator struct {
	model        string
	text         strings.Builder
	thinking     strings.Builder
	tools        []accumulatedTool
	stopReason   string
	inputTokens  int
	outputTokens int
}

type accumulatedTool struct {
	index int
	id    string
	name  string
	args  strings.Builder
}

func (a *messageAccumulator) addText(text string) { a.text.WriteString(text) }

func (a *messageAccumulator) addThinking(thinking string) {
	a.thinking.WriteString(thinking)
}

// addToolArgs records a tool call and appends its argument fragment. Streaming
// upstreams deliver one fragment per delta, so fragments accumulate on the
// entry created for the same choice index; a non-streaming response delivers
// the complete arguments in one call.
func (a *messageAccumulator) addToolArgs(index int, id, name, args string) {
	for i := range a.tools {
		if a.tools[i].index != index {
			continue
		}
		if id != "" {
			a.tools[i].id = id
		}
		if name != "" {
			a.tools[i].name = name
		}
		a.tools[i].args.WriteString(args)
		return
	}
	a.tools = append(a.tools, accumulatedTool{index: index, id: id, name: name})
	a.tools[len(a.tools)-1].args.WriteString(args)
}

func (a *messageAccumulator) setUsage(inputTokens, outputTokens int) {
	a.inputTokens = inputTokens
	a.outputTokens = outputTokens
}

// message renders the accumulated state as an Anthropic Message document.
func (a *messageAccumulator) message(id, modelOverride string) ([]byte, error) {
	model := a.model
	if modelOverride != "" {
		model = modelOverride
	}
	content := []any{}
	if a.thinking.Len() > 0 {
		content = append(content, map[string]any{
			"type":     "thinking",
			"thinking": a.thinking.String(),
		})
	}
	if a.text.Len() > 0 {
		content = append(content, map[string]any{
			"type": "text",
			"text": a.text.String(),
		})
	}
	for _, t := range a.tools {
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    t.id,
			"name":  t.name,
			"input": decodeToolInput(t.args.String()),
		})
	}
	stopReason := a.stopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}
	out, err := json.Marshal(map[string]any{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]int{
			"input_tokens":  a.inputTokens,
			"output_tokens": a.outputTokens,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("translate: marshal anthropic message: %w", err)
	}
	return out, nil
}

// decodeToolInput parses accumulated tool-call arguments into the JSON object
// Anthropic expects for a tool_use block. Empty arguments yield an empty
// object. Arguments that are not valid JSON (an upstream that closed the
// stream mid-argument) are preserved under a single `_raw` key rather than
// dropped, so the tool result the client sends back still carries them.
func decodeToolInput(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	if !json.Valid([]byte(raw)) {
		return map[string]any{"_raw": raw}
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return map[string]any{"_raw": raw}
	}
	return v
}

// decodeJSONObject decodes body into a map with integer literals preserved as
// json.Number.
func decodeJSONObject(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("body is not a JSON object")
	}
	return obj, nil
}

// clampJSONInt lowers an integral object field to limit when it exceeds it.
// Absent or non-integral fields are left untouched.
func clampJSONInt(obj map[string]any, key string, limit int) {
	num, ok := obj[key].(json.Number)
	if !ok {
		return
	}
	v, err := num.Int64()
	if err != nil {
		return
	}
	if v > int64(limit) {
		obj[key] = limit
	}
}
