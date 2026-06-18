package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeNIMBody_NoTools(t *testing.T) {
	in := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`)
	out, err := sanitizeNIMBody(in)
	if err != nil {
		t.Fatal(err)
	}
	var gotIn, gotOut map[string]any
	if err := json.Unmarshal(in, &gotIn); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &gotOut); err != nil {
		t.Fatal(err)
	}
	if gotIn["model"] != gotOut["model"] {
		t.Errorf("model: got %v, want %v", gotOut["model"], gotIn["model"])
	}
	inMsgs := gotIn["messages"].([]any)
	outMsgs := gotOut["messages"].([]any)
	if len(inMsgs) != len(outMsgs) {
		t.Errorf("messages length: got %d, want %d", len(outMsgs), len(inMsgs))
	}
}

func TestSanitizeNIMBody_StripsBooleanAdditionalPropertiesTrue(t *testing.T) {
	in := []byte(
		`{"model":"x","messages":[],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","properties":{"x":{"type":"string"}},"additionalProperties":true}}}]}`,
	)
	out, err := sanitizeNIMBody(in)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	tools := got["tools"].([]any)
	params := tools[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if _, ok := params["additionalProperties"]; ok {
		t.Errorf("expected additionalProperties stripped, got %v", params["additionalProperties"])
	}
}

func TestSanitizeNIMBody_StripsBooleanAdditionalPropertiesFalse(t *testing.T) {
	in := []byte(
		`{"model":"x","messages":[],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","additionalProperties":false}}}]}`,
	)
	out, err := sanitizeNIMBody(in)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	tools := got["tools"].([]any)
	params := tools[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if _, ok := params["additionalProperties"]; ok {
		t.Errorf("expected additionalProperties stripped, got %v", params["additionalProperties"])
	}
}

func TestSanitizeNIMBody_PreservesObjectAdditionalProperties(t *testing.T) {
	in := []byte(
		`{"model":"x","messages":[],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","additionalProperties":{"type":"string"}}}}]}`,
	)
	out, err := sanitizeNIMBody(in)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	tools := got["tools"].([]any)
	params := tools[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	ap, ok := params["additionalProperties"].(map[string]any)
	if !ok {
		t.Fatalf(
			"expected object additionalProperties preserved, got %T",
			params["additionalProperties"],
		)
	}
	if ap["type"] != "string" {
		t.Errorf("expected type=string, got %v", ap["type"])
	}
}

func TestSanitizeNIMBody_RenamesTypeParam(t *testing.T) {
	in := []byte(
		`{"model":"x","messages":[],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","properties":{"type":{"type":"string"}}}}}]}`,
	)
	out, err := sanitizeNIMBody(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"_fcc_arg_type"`) {
		t.Errorf("expected _fcc_arg_type alias, got %s", string(out))
	}
	if strings.Contains(string(out), `"properties":{"type":`) {
		t.Errorf("expected 'type' param renamed, got %s", string(out))
	}
}

func TestSanitizeNIMBody_RenamesNestedTypeParams(t *testing.T) {
	in := []byte(
		`{"model":"x","messages":[],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","properties":{"outer":{"type":"object","properties":{"type":{"type":"string"}}}}}}}]}`,
	)
	out, err := sanitizeNIMBody(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), `"_fcc_arg_type"`) < 1 {
		t.Errorf("expected at least one _fcc_arg_type alias, got %s", string(out))
	}
}

func TestSanitizeNIMBody_PreservesTypeJSONSchemaKey(t *testing.T) {
	in := []byte(
		`{"model":"x","messages":[],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","properties":{"x":{"type":"string"}}}}}]}`,
	)
	out, err := sanitizeNIMBody(in)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	tools := got["tools"].([]any)
	params := tools[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Errorf("expected type=object preserved as JSON Schema key, got %v", params["type"])
	}
}
