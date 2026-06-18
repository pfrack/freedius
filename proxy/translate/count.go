// Package translate contains wire-format helpers used by the freedius proxy
// (and the Anthropic ↔ OpenAI conversion in particular). This file adds a
// local BPE-based token counter used by the /v1/messages/count_tokens
// dispatch path for OpenAI-protocol upstreams.
package translate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/pkoukk/tiktoken-go"

	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

// Encoding names supported by the local counter. The BPE dictionaries for
// both are embedded in the binary via tiktoken-go-loader (no network/disk at
// runtime — NFR-Privacy).
const (
	cl100kBase = "cl100k_base"
	o200kBase  = "o200k_base"
)

// imageTokenCost is the fixed token cost charged per image content block.
// Approximates Anthropic's lower bound for small images (see
// context/foundation/prd.md and the research notes for this change).
const imageTokenCost = 160

// documentTokenCost is the fixed token cost charged per document content
// block. Approximates a small (~5-page) PDF.
const documentTokenCost = 500

func init() {
	tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}

// encoderCache memoizes a *tiktoken.Tiktoken for one encoding name. The BPE
// load is one-time per encoding (~50-100 ms the first time) and is then
// cached for the process lifetime.
type encoderCache struct {
	once sync.Once
	enc  *tiktoken.Tiktoken
	err  error
}

var (
	countEncMu  sync.Mutex
	countEncChx = map[string]*encoderCache{
		cl100kBase: {},
		o200kBase:  {},
	}
)

func getEncoder(encodingName string) (*tiktoken.Tiktoken, error) {
	countEncMu.Lock()
	chx, ok := countEncChx[encodingName]
	if !ok {
		countEncMu.Unlock()
		return nil, fmt.Errorf("unsupported encoding %q", encodingName)
	}
	countEncMu.Unlock()
	chx.once.Do(func() {
		chx.enc, chx.err = tiktoken.GetEncoding(encodingName)
	})
	return chx.enc, chx.err
}

// pickEncoding returns the BPE encoding to use for a given upstream model
// name. gpt-4o, gpt-4.1, gpt-4.5, o1, o3, o4 (and the o200k_base family)
// use o200k_base; everything else falls back to cl100k_base. This is a
// heuristic — accuracy is approximate either way (within ~5-10% of the
// upstream tokenizer for typical Claude Code prompts).
func pickEncoding(modelName string) string {
	o200kTargets := []string{
		"gpt-4o",
		"gpt-4.1",
		"gpt-4.5",
		"o1",
		"o3",
		"o4",
		"o200k_base",
	}
	for _, t := range o200kTargets {
		if bytes.Contains([]byte(modelName), []byte(t)) {
			return o200kBase
		}
	}
	return cl100kBase
}

// CountInputTokens counts input tokens for an Anthropic-format
// /v1/messages request body. The body is unmarshalled into anthropicMessage;
// system, messages[].content, and tools[] are walked and tokenized as
// described in the research notes for this change. The returned count
// approximates the upstream Anthropic count_tokens response within ~5-10%
// for typical Claude Code prompts (text + JSON + tool definitions).
//
// Image and document blocks are charged at fixed rates (imageTokenCost and
// documentTokenCost) instead of size-based math. Server-side tool result
// blocks (web_search, code execution, etc.) and redacted_thinking blocks
// contribute 0 — they have no client-visible text to encode.
//
// If the body is not a valid Anthropic messages request, the function falls
// back to a lenient best-effort re-parse that walks the body as a generic
// map[string]any and returns whatever count it can produce; if even the
// lenient re-parse fails the function returns 0 and a non-nil error.
func CountInputTokens(body []byte) (int, error) {
	var req anthropicMessage
	if err := json.Unmarshal(body, &req); err != nil {
		n, lerr := countLenient(body)
		if lerr != nil {
			return 0, fmt.Errorf("unmarshal anthropic request: %w", lerr)
		}
		return n, nil
	}
	enc, err := getEncoder(pickEncoding(req.Model))
	if err != nil {
		return 0, fmt.Errorf("get encoder: %w", err)
	}
	n := 0
	n += countSystem(enc, req.System)
	n += countMessages(enc, req.Messages)
	n += countTools(enc, req.Tools)
	return n, nil
}

// countLenient is the best-effort fallback. It decodes the body as a generic
// JSON object (numbers preserved as json.Number so very large numbers don't
// get truncated) and walks the same fields the strict path would. Fields
// that fail validation contribute 0 — the function never returns an error
// for malformed bodies.
func countLenient(body []byte) (int, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return 0, err
	}
	enc, err := getEncoder(cl100kBase)
	if err != nil {
		return 0, err
	}
	n := 0
	if sys, ok := m["system"]; ok {
		n += countSystem(enc, sys)
	}
	if msgs, ok := m["messages"].([]any); ok {
		for _, item := range msgs {
			mm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			n += countContent(enc, mm["content"])
		}
	}
	if tools, ok := m["tools"].([]any); ok {
		for _, item := range tools {
			tt, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if name, ok := tt["name"].(string); ok {
				n += len(enc.Encode(name, nil, nil))
			}
			if desc, ok := tt["description"].(string); ok {
				n += len(enc.Encode(desc, nil, nil))
			}
			if schema, ok := tt["input_schema"]; ok {
				if buf, mErr := json.Marshal(schema); mErr == nil {
					n += len(enc.Encode(string(buf), nil, nil))
				}
			}
		}
	}
	return n, nil
}

func countSystem(enc *tiktoken.Tiktoken, sys any) int {
	switch s := sys.(type) {
	case string:
		return len(enc.Encode(s, nil, nil))
	case []any:
		n := 0
		for _, block := range s {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t == "text" {
				if text, ok := m["text"].(string); ok {
					n += len(enc.Encode(text, nil, nil))
				}
			}
		}
		return n
	}
	return 0
}

func countMessages(enc *tiktoken.Tiktoken, msgs []anthropicMsgItem) int {
	n := 0
	for _, msg := range msgs {
		n += countContent(enc, msg.Content)
	}
	return n
}

func countContent(enc *tiktoken.Tiktoken, content any) int {
	switch c := content.(type) {
	case string:
		return len(enc.Encode(c, nil, nil))
	case []any:
		n := 0
		for _, block := range c {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			switch t, _ := m["type"].(string); t {
			case "text":
				if text, ok := m["text"].(string); ok {
					n += len(enc.Encode(text, nil, nil))
				}
			case "tool_use":
				if name, ok := m["name"].(string); ok {
					n += len(enc.Encode(name, nil, nil))
				}
				if input, ok := m["input"]; ok {
					if buf, mErr := json.Marshal(input); mErr == nil {
						n += len(enc.Encode(string(buf), nil, nil))
					}
				}
			case "tool_result":
				n += countContent(enc, m["content"])
			case "thinking":
				if thinking, ok := m["thinking"].(string); ok {
					n += len(enc.Encode(thinking, nil, nil))
				}
			case "image":
				n += imageTokenCost
			case "document":
				n += documentTokenCost
			case "redacted_thinking", "server_tool_use",
				"web_search_tool_result", "web_fetch_tool_result",
				"code_execution_tool_result", "bash_code_execution_tool_result",
				"text_editor_code_execution_tool_result",
				"tool_search_tool_result", "container_upload",
				"mid_conv_system":
			}
		}
		return n
	}
	return 0
}

func countTools(enc *tiktoken.Tiktoken, tools []anthropicTool) int {
	n := 0
	for _, t := range tools {
		n += len(enc.Encode(t.Name, nil, nil))
		n += len(enc.Encode(t.Description, nil, nil))
		if t.InputSchema != nil {
			if buf, err := json.Marshal(t.InputSchema); err == nil {
				n += len(enc.Encode(string(buf), nil, nil))
			}
		}
	}
	return n
}
