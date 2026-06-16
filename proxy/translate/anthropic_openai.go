package translate

import (
	"encoding/json"
	"fmt"
)

func TranslateRequest(anthropicBody []byte, targetModel string) ([]byte, error) {
	var req anthropicRequest
	if err := json.Unmarshal(anthropicBody, &req); err != nil {
		return nil, fmt.Errorf("translate: parse anthropic request: %w", err)
	}

	out := openAIRequest{
		Model:       targetModel,
		MaxTokens:   &req.MaxTokens,
		Tools:       nil,
		ToolChoice:  translateToolChoice(req.ToolChoice),
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	if len(req.StopSequences) > 0 {
		out.Stop = req.StopSequences
	}

	if req.Stream {
		out.StreamOptions = &openAIStreamOptions{IncludeUsage: true}
	}

	out.Messages = make([]openAIMessage, 0, len(req.Messages)+1)

	if sysMsg, ok := translateSystem(req.System); ok {
		out.Messages = append(out.Messages, sysMsg)
	}

	for _, m := range req.Messages {
		translated, err := translateMessage(m)
		if err != nil {
			return nil, fmt.Errorf("translate: message: %w", err)
		}
		out.Messages = append(out.Messages, translated...)
	}

	if len(req.Tools) > 0 {
		out.Tools = make([]openAITool, 0, len(req.Tools))
		for _, t := range req.Tools {
			out.Tools = append(out.Tools, openAITool{
				Type: "function",
				Function: openAIToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			})
		}
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("translate: encode openai request: %w", err)
	}
	return encoded, nil
}

func translateSystem(sys any) (openAIMessage, bool) {
	if sys == nil {
		return openAIMessage{}, false
	}
	switch v := sys.(type) {
	case string:
		if v == "" {
			return openAIMessage{}, false
		}
		return openAIMessage{Role: "system", Content: v}, true
	case []any:
		var text string
		for _, block := range v {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := b["type"].(string); t == "text" {
				if s, ok := b["text"].(string); ok {
					if text != "" {
						text += "\n"
					}
					text += s
				}
			}
		}
		if text == "" {
			return openAIMessage{}, false
		}
		return openAIMessage{Role: "system", Content: text}, true
	}
	return openAIMessage{}, false
}

func translateMessage(m anthropicMessage) ([]openAIMessage, error) {
	switch m.Role {
	case "user":
		return translateUserMessage(m.Content)
	case "assistant":
		return translateAssistantMessage(m.Content)
	case "system":
		return []openAIMessage{{Role: "system", Content: stringifyContent(m.Content)}}, nil
	}
	return nil, fmt.Errorf("unknown role %q", m.Role)
}

func translateUserMessage(content any) ([]openAIMessage, error) {
	switch v := content.(type) {
	case string:
		return []openAIMessage{{Role: "user", Content: v}}, nil
	case []any:
		var toolResults []openAIMessage
		var parts []any
		for _, block := range v {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			t, _ := b["type"].(string)
			switch t {
			case "text":
				if s, ok := b["text"].(string); ok {
					parts = append(parts, map[string]any{"type": "text", "text": s})
				}
			case "image":
				parts = append(parts, translateImage(b))
			case "tool_result":
				id, _ := b["tool_use_id"].(string)
				c := stringifyContent(b["content"])
				toolResults = append(toolResults, openAIMessage{Role: "tool", ToolCallID: id, Content: c})
			}
		}
		out := make([]openAIMessage, 0, 1+len(toolResults))
		if len(parts) > 0 {
			out = append(out, openAIMessage{Role: "user", Content: parts})
		}
		out = append(out, toolResults...)
		return out, nil
	}
	return nil, fmt.Errorf("user content: unsupported type %T", content)
}

func translateAssistantMessage(content any) ([]openAIMessage, error) {
	switch v := content.(type) {
	case string:
		return []openAIMessage{{Role: "assistant", Content: v}}, nil
	case []any:
		var textParts []string
		var toolCalls []openAIToolCall
		for _, block := range v {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			t, _ := b["type"].(string)
			switch t {
			case "text":
				if s, ok := b["text"].(string); ok {
					textParts = append(textParts, s)
				}
			case "tool_use":
				id, _ := b["id"].(string)
				name, _ := b["name"].(string)
				input := b["input"]
				var args string
				if input != nil {
					encoded, err := json.Marshal(input)
					if err != nil {
						return nil, fmt.Errorf("tool_use input marshal: %w", err)
					}
					if string(encoded) != "null" {
						args = string(encoded)
					}
				}
				toolCalls = append(toolCalls, openAIToolCall{
					Index: len(toolCalls),
					ID:    id,
					Type:  "function",
					Function: openAIToolCallFunction{
						Name:      name,
						Arguments: args,
					},
				})
			}
		}
		msg := openAIMessage{Role: "assistant", Content: joinStrings(textParts, "")}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
		return []openAIMessage{msg}, nil
	}
	return nil, fmt.Errorf("assistant content: unsupported type %T", content)
}

func translateImage(b map[string]any) map[string]any {
	src, _ := b["source"].(map[string]any)
	if src == nil {
		return map[string]any{"type": "text", "text": "[unsupported image]"}
	}
	mediaType, _ := src["media_type"].(string)
	data, _ := src["data"].(string)
	if mediaType == "" || data == "" {
		return map[string]any{"type": "text", "text": "[unsupported image]"}
	}
	return map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": fmt.Sprintf("data:%s;base64,%s", mediaType, data),
		},
	}
}

func translateToolChoice(tc any) any {
	if tc == nil {
		return nil
	}
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return "auto"
		case "any":
			return "required"
		case "none":
			return "none"
		}
		return v
	case map[string]any:
		t, _ := v["type"].(string)
		if t == "tool" {
			name, _ := v["name"].(string)
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": name,
				},
			}
		}
		if t == "any" {
			return "required"
		}
	}
	return tc
}

func stringifyContent(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, b := range x {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			t, _ := bm["type"].(string)
			if t == "text" {
				if s, ok := bm["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return joinStrings(parts, "\n")
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}
