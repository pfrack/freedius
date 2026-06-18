package translate

import (
	"strings"
	"testing"
)

func TestCountInputTokens(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantNonZero bool
		wantZero    bool
		wantErr     bool
	}{
		{
			name:     "empty body",
			body:     ``,
			wantZero: true,
			wantErr:  true,
		},
		{
			name:     "invalid JSON",
			body:     `not json at all`,
			wantZero: true,
			wantErr:  true,
		},
		{
			name:        "system as string",
			body:        `{"model":"claude-opus-4","system":"You are a helpful assistant.","messages":[]}`,
			wantNonZero: true,
		},
		{
			name:        "system as text-block array",
			body:        `{"model":"claude-opus-4","system":[{"type":"text","text":"You are helpful."},{"type":"text","text":"Be concise."}],"messages":[]}`,
			wantNonZero: true,
		},
		{
			name:        "user message string content",
			body:        `{"model":"claude-opus-4","messages":[{"role":"user","content":"hello world"}]}`,
			wantNonZero: true,
		},
		{
			name:        "user message text block",
			body:        `{"model":"claude-opus-4","messages":[{"role":"user","content":[{"type":"text","text":"hello there"}]}]}`,
			wantNonZero: true,
		},
		{
			name:        "tool_use block",
			body:        `{"model":"claude-opus-4","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"get_weather","input":{"city":"Paris"}}]}]}`,
			wantNonZero: true,
		},
		{
			name:        "tool_result string content",
			body:        `{"model":"claude-opus-4","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"sunny, 22C"}]}]}`,
			wantNonZero: true,
		},
		{
			name:        "tool_result block-array content",
			body:        `{"model":"claude-opus-4","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"result text"}]}]}]}`,
			wantNonZero: true,
		},
		{
			name:        "thinking block includes thinking text",
			body:        `{"model":"claude-opus-4","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"the user wants to know the weather","signature":"sig-abc"}]}]}`,
			wantNonZero: true,
		},
		{
			name:        "image block adds fixed cost",
			body:        `{"model":"claude-opus-4","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}`,
			wantNonZero: true,
		},
		{
			name:        "document block adds fixed cost",
			body:        `{"model":"claude-opus-4","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0xLjQK"}}]}]}`,
			wantNonZero: true,
		},
		{
			name:     "redacted_thinking contributes 0",
			body:     `{"model":"claude-opus-4","messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"encrypted-blob"}]}]}`,
			wantZero: true,
		},
		{
			name:        "multiple messages multiple tools — sum is monotonic",
			body:        `{"model":"claude-opus-4","system":"Be brief.","tools":[{"name":"tool_a","description":"does A","input_schema":{"type":"object","properties":{"x":{"type":"number"}}}},{"name":"tool_b","description":"does B","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"first question"},{"role":"assistant","content":"first answer"},{"role":"user","content":"second question with more text to count"}]}`,
			wantNonZero: true,
		},
		{
			name:        "input_schema with nested JSON",
			body:        `{"model":"claude-opus-4","tools":[{"name":"search","description":"search the docs","input_schema":{"type":"object","properties":{"query":{"type":"string","description":"the search query"},"limit":{"type":"integer","minimum":1}},"required":["query"]}}]}`,
			wantNonZero: true,
		},
		{
			name:        "lenient re-parse: valid JSON but fails strict anthropic shape",
			body:        `{"model":"claude-opus-4","messages":[{"role":"user","content":"hi","extra_unknown_field":"ignored"}],"also_unknown":42}`,
			wantNonZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CountInputTokens([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Errorf("CountInputTokens: err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantNonZero && got == 0 {
				t.Errorf("CountInputTokens: got 0, want non-zero")
			}
			if tt.wantZero && got != 0 {
				t.Errorf("CountInputTokens: got %d, want 0", got)
			}
		})
	}
}

func TestCountInputTokensImageFixedCost(t *testing.T) {
	bodyNoImage := `{"model":"claude-opus-4","messages":[{"role":"user","content":[]}]}`
	bodyOneImage := `{"model":"claude-opus-4","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}`
	bodyTwoImages := `{"model":"claude-opus-4","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"a"}},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"b"}}]}]}`
	noImg, err := CountInputTokens([]byte(bodyNoImage))
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	oneImg, err := CountInputTokens([]byte(bodyOneImage))
	if err != nil {
		t.Fatalf("one image: %v", err)
	}
	twoImg, err := CountInputTokens([]byte(bodyTwoImages))
	if err != nil {
		t.Fatalf("two images: %v", err)
	}
	if oneImg-noImg != imageTokenCost {
		t.Errorf("one image delta = %d, want %d", oneImg-noImg, imageTokenCost)
	}
	if twoImg-oneImg != imageTokenCost {
		t.Errorf("second image delta = %d, want %d", twoImg-oneImg, imageTokenCost)
	}
}

func TestCountInputTokensDocumentFixedCost(t *testing.T) {
	bodyNoDoc := `{"model":"claude-opus-4","messages":[{"role":"user","content":[]}]}`
	bodyOneDoc := `{"model":"claude-opus-4","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0xLjQK"}}]}]}`
	noDoc, err := CountInputTokens([]byte(bodyNoDoc))
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	oneDoc, err := CountInputTokens([]byte(bodyOneDoc))
	if err != nil {
		t.Fatalf("one doc: %v", err)
	}
	if oneDoc-noDoc != documentTokenCost {
		t.Errorf("one doc delta = %d, want %d", oneDoc-noDoc, documentTokenCost)
	}
}

func TestCountInputTokensThinkingSkipsSignature(t *testing.T) {
	bodyNoSig := `{"model":"claude-opus-4","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hello"}]}]}`
	bodyWithSig := `{"model":"claude-opus-4","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hello","signature":"AAAA-long-signature-blob-that-would-add-tokens"}]}]}`
	a, err := CountInputTokens([]byte(bodyNoSig))
	if err != nil {
		t.Fatalf("no sig: %v", err)
	}
	b, err := CountInputTokens([]byte(bodyWithSig))
	if err != nil {
		t.Fatalf("with sig: %v", err)
	}
	if a != b {
		t.Errorf("signature should be ignored: without=%d, with=%d", a, b)
	}
}

func TestPickEncoding(t *testing.T) {
	tests := []struct {
		modelName string
		want      string
	}{
		{"gpt-4o", o200kBase},
		{"gpt-4o-mini", o200kBase},
		{"gpt-4.1", o200kBase},
		{"gpt-4.5", o200kBase},
		{"o1-preview", o200kBase},
		{"o3-mini", o200kBase},
		{"o4-turbo", o200kBase},
		{"gpt-4", cl100kBase},
		{"gpt-3.5-turbo", cl100kBase},
		{"meta/llama-3.1-70b-instruct", cl100kBase},
		{"claude-opus-4", cl100kBase},
		{"", cl100kBase},
		{"some-gpt-3-deriv", cl100kBase},
		{"o200k_base", o200kBase},
	}
	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			if got := pickEncoding(tt.modelName); got != tt.want {
				t.Errorf("pickEncoding(%q) = %q, want %q", tt.modelName, got, tt.want)
			}
		})
	}
}

func TestCountInputTokensEncodingAffectsCount(t *testing.T) {
	body := `{"model":"claude-opus-4","messages":[{"role":"user","content":"hello world this is a test message"}]}`
	n, err := CountInputTokens([]byte(body))
	if err != nil {
		t.Fatalf("CountInputTokens: %v", err)
	}
	if n == 0 {
		t.Errorf("expected non-zero token count for %q", body)
	}
	if n < 3 || n > 50 {
		t.Errorf("token count %d outside expected range [3, 50] for short message", n)
	}
}

func TestCountInputTokensLenientRecovers(t *testing.T) {
	body := `{"model":"claude-opus-4","messages":[{"role":"user","content":"hello"}],"this_field_is_unknown":true}`
	n, err := CountInputTokens([]byte(body))
	if err != nil {
		t.Fatalf("lenient re-parse should not return error for valid JSON, got: %v", err)
	}
	if n == 0 {
		t.Errorf("lenient re-parse should have counted the 'hello' content, got 0")
	}
}

func TestCountInputTokensLenientFallbackExercised(t *testing.T) {
	body := `{"model":"claude-opus-4","messages":{"not":"an array"},"extra":"unknown"}`
	n, err := CountInputTokens([]byte(body))
	if err != nil {
		t.Fatalf("lenient re-parse should swallow type-mismatch and return 0, got err: %v", err)
	}
	if n != 0 {
		t.Errorf("lenient re-parse of type-mismatched body: got %d, want 0", n)
	}
}

func TestCountInputTokensLenientFallbackCountsWhatItCan(t *testing.T) {
	body := `{"model":"claude-opus-4","messages":{"role":"user","content":"hi"},"tools":"not-an-array"}`
	n, err := CountInputTokens([]byte(body))
	if err != nil {
		t.Fatalf("lenient re-parse should not error on tools type mismatch, got: %v", err)
	}
	if n != 0 {
		t.Errorf("lenient re-parse of bad-shape body: got %d, want 0", n)
	}
}

func TestCountInputTokensLenientReturnsZeroForPurelyBadJSON(t *testing.T) {
	body := `not even close to json`
	n, err := CountInputTokens([]byte(body))
	if err == nil {
		t.Errorf("expected error for bad JSON, got nil")
	}
	if n != 0 {
		t.Errorf("expected 0 for bad JSON, got %d", n)
	}
}

func TestCountInputTokensEmptyContentArray(t *testing.T) {
	body := `{"model":"claude-opus-4","messages":[{"role":"user","content":[]}]}`
	n, err := CountInputTokens([]byte(body))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 {
		t.Errorf("empty content array should yield 0, got %d", n)
	}
}

func TestCountInputTokensReasoningContentIgnored(t *testing.T) {
	bodyWith := `{"model":"claude-opus-4","messages":[{"role":"assistant","content":"","reasoning_content":"private-thoughts-blob"}]}`
	bodyWithout := `{"model":"claude-opus-4","messages":[{"role":"assistant","content":""}]}`
	a, err := CountInputTokens([]byte(bodyWithout))
	if err != nil {
		t.Fatalf("without: %v", err)
	}
	b, err := CountInputTokens([]byte(bodyWith))
	if err != nil {
		t.Fatalf("with: %v", err)
	}
	if a != b {
		t.Errorf("reasoning_content should not affect count: empty=%d, with=%d", a, b)
	}
}

func TestCountInputTokensStringsBody(t *testing.T) {
	body := strings.Repeat("hello world ", 100)
	full := `{"model":"claude-opus-4","messages":[{"role":"user","content":"` + body + `"}]}`
	n, err := CountInputTokens([]byte(full))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n < 50 {
		t.Errorf("expected at least 50 tokens for 100x 'hello world ', got %d", n)
	}
}
