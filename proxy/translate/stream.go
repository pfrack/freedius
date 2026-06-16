package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func TranslateStream(upstream io.Reader, downstream io.Writer, flush func() error) error {
	br := bufio.NewReaderSize(upstream, 64*1024)
	emitter := newAnthropicEmitter()

	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			if bytes.HasPrefix(trimmed, []byte("data: ")) {
				data := bytes.TrimPrefix(trimmed, []byte("data: "))
				if bytes.Equal(data, []byte("[DONE]")) {
					if emitter.messageStopPending {
						if err := emitter.emitPendingMessageDelta(downstream, flush); err != nil {
							return err
						}
					}
					if err := emitter.emitMessageStop(downstream, flush); err != nil {
						return err
					}
					return nil
				}
				var chunk openAIChunk
				if err := json.Unmarshal(data, &chunk); err != nil {
					return fmt.Errorf("translate: parse openai chunk: %w", err)
				}
				events, err := emitter.consumeChunk(chunk)
				if err != nil {
					return err
				}
				for _, ev := range events {
					if _, err := downstream.Write(ev); err != nil {
						return err
					}
					if flush != nil {
						if err := flush(); err != nil {
							return err
						}
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

type toolAccum struct {
	anthropicIndex int
	id             string
	name           string
}

type anthropicEmitter struct {
	started            bool
	blockIdx           int
	currentType        string
	tools              map[int]*toolAccum
	messageID          string
	model              string
	outputTokens       int
	finishReason       *string
	messageStopPending bool
}

func newAnthropicEmitter() *anthropicEmitter {
	return &anthropicEmitter{tools: map[int]*toolAccum{}}
}

func (e *anthropicEmitter) consumeChunk(chunk openAIChunk) ([][]byte, error) {
	var out [][]byte

	if chunk.Usage != nil {
		e.outputTokens = chunk.Usage.CompletionTokens
	}

	if e.messageStopPending {
		ev := e.buildMessageDelta()
		encoded, err := encodeSSE("message_delta", ev)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
		e.messageStopPending = false
	}

	if !e.started {
		if chunk.ID != "" {
			e.messageID = chunk.ID
		} else {
			e.messageID = "msg_freedius"
		}
		e.model = chunk.Model
		ev := e.buildMessageStart()
		encoded, err := encodeSSE("message_start", ev)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
		e.started = true
	}

	for _, choice := range chunk.Choices {
		if choice.Delta.Role != "" {
			continue
		}
		if choice.Delta.Content != "" {
			if e.currentType != "text" {
				if e.currentType != "" {
					stop := e.buildContentBlockStop()
					encoded, err := encodeSSE("content_block_stop", stop)
					if err != nil {
						return nil, err
					}
					out = append(out, encoded)
				}
				e.blockIdx++
				e.currentType = "text"
				start := contentBlockStartEvent{
					Type:  "content_block_start",
					Index: e.blockIdx,
					ContentBlock: textBlock{
						Type: "text",
						Text: "",
					},
				}
				encoded, err := encodeSSE("content_block_start", start)
				if err != nil {
					return nil, err
				}
				out = append(out, encoded)
			}
			ev := contentBlockDeltaEvent{
				Type:  "content_block_delta",
				Index: e.blockIdx,
				Delta: textDelta{
					Type: "text_delta",
					Text: choice.Delta.Content,
				},
			}
			encoded, err := encodeSSE("content_block_delta", ev)
			if err != nil {
				return nil, err
			}
			out = append(out, encoded)
		}
		for _, tc := range choice.Delta.ToolCalls {
			state, exists := e.tools[tc.Index]
			if !exists {
				if e.currentType != "" {
					stop := e.buildContentBlockStop()
					encoded, err := encodeSSE("content_block_stop", stop)
					if err != nil {
						return nil, err
					}
					out = append(out, encoded)
				}
				e.blockIdx++
				e.currentType = "tool"
				state = &toolAccum{anthropicIndex: e.blockIdx}
				e.tools[tc.Index] = state
				if tc.ID != "" {
					state.id = tc.ID
				}
				if tc.Function.Name != "" {
					state.name = tc.Function.Name
				}
				start := contentBlockStartEvent{
					Type:  "content_block_start",
					Index: e.blockIdx,
					ContentBlock: toolUseBlock{
						Type:  "tool_use",
						ID:    state.id,
						Name:  state.name,
						Input: map[string]any{},
					},
				}
				encoded, err := encodeSSE("content_block_start", start)
				if err != nil {
					return nil, err
				}
				out = append(out, encoded)
			}
			if tc.ID != "" {
				state.id = tc.ID
			}
			if tc.Function.Name != "" {
				state.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				ev := contentBlockDeltaEvent{
					Type:  "content_block_delta",
					Index: state.anthropicIndex,
					Delta: inputJSONDelta{
						Type:        "input_json_delta",
						PartialJSON: tc.Function.Arguments,
					},
				}
				encoded, err := encodeSSE("content_block_delta", ev)
				if err != nil {
					return nil, err
				}
				out = append(out, encoded)
			}
		}
		if choice.FinishReason != nil {
			if e.currentType != "" {
				stop := e.buildContentBlockStop()
				encoded, err := encodeSSE("content_block_stop", stop)
				if err != nil {
					return nil, err
				}
				out = append(out, encoded)
				e.currentType = ""
			}
			fr := *choice.FinishReason
			e.finishReason = &fr
			e.messageStopPending = true
		}
	}

	return out, nil
}

func (e *anthropicEmitter) emitMessageStop(w io.Writer, flush func() error) error {
	encoded, err := encodeSSE("message_stop", messageStopEvent{Type: "message_stop"})
	if err != nil {
		return err
	}
	if _, err := w.Write(encoded); err != nil {
		return err
	}
	if flush != nil {
		return flush()
	}
	return nil
}

func (e *anthropicEmitter) emitPendingMessageDelta(w io.Writer, flush func() error) error {
	ev := e.buildMessageDelta()
	encoded, err := encodeSSE("message_delta", ev)
	if err != nil {
		return err
	}
	if _, err := w.Write(encoded); err != nil {
		return err
	}
	if flush != nil {
		return flush()
	}
	return nil
}

func (e *anthropicEmitter) buildMessageStart() messageStartEvent {
	var ev messageStartEvent
	ev.Type = "message_start"
	ev.Message.ID = e.messageID
	ev.Message.Model = e.model
	ev.Message.Role = "assistant"
	ev.Message.Usage.InputTokens = 0
	ev.Message.Usage.OutputTokens = 1
	return ev
}

func (e *anthropicEmitter) buildContentBlockStop() contentBlockStopEvent {
	return contentBlockStopEvent{
		Type:  "content_block_stop",
		Index: e.blockIdx,
	}
}

func (e *anthropicEmitter) buildMessageDelta() messageDeltaEvent {
	var ev messageDeltaEvent
	ev.Type = "message_delta"
	reason := translateFinishReason(e.finishReason)
	ev.Delta.StopReason = &reason
	ev.Usage.OutputTokens = e.outputTokens
	return ev
}

func translateFinishReason(fr *string) string {
	if fr == nil {
		return "end_turn"
	}
	switch *fr {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	}
	return "end_turn"
}

func encodeSSE(eventType string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("translate: marshal %s event: %w", eventType, err)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "event: %s\ndata: %s\n\n", eventType, raw)
	return buf.Bytes(), nil
}
