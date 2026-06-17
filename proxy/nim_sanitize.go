package proxy

import "encoding/json"

func sanitizeNIMBody(openAIBody []byte) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(openAIBody, &body); err != nil {
		return nil, err
	}
	if tools, ok := body["tools"].([]any); ok {
		for _, t := range tools {
			tool, ok := t.(map[string]any)
			if !ok {
				continue
			}
			function, ok := tool["function"].(map[string]any)
			if !ok {
				continue
			}
			if params, ok := function["parameters"].(map[string]any); ok {
				function["parameters"] = sanitizeSchema(params)
			}
		}
	}
	return json.Marshal(body)
}

func sanitizeSchema(node map[string]any) map[string]any {
	stripBooleanSubschemas(node)
	aliasTypeParams(node)
	for k, v := range node {
		if child, ok := v.(map[string]any); ok {
			node[k] = sanitizeSchema(child)
		}
	}
	return node
}

func stripBooleanSubschemas(node map[string]any) {
	for _, key := range []string{"properties", "additionalProperties", "items", "anyOf", "oneOf", "allOf"} {
		if v, ok := node[key]; ok {
			if _, isBool := v.(bool); isBool {
				delete(node, key)
			}
		}
	}
}

func aliasTypeParams(node map[string]any) {
	if props, ok := node["properties"].(map[string]any); ok {
		if t, exists := props["type"]; exists {
			delete(props, "type")
			props["_fcc_arg_type"] = t
		}
	}
	for _, key := range []string{"properties", "additionalProperties", "items"} {
		v, ok := node[key]
		if !ok {
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			aliasTypeParams(child)
		case []any:
			for _, item := range child {
				if m, ok := item.(map[string]any); ok {
					aliasTypeParams(m)
				}
			}
		}
	}
}
