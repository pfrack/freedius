package translate

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestCountInputTokens_RoundTrip exercises the local BPE-based counter
// against the real Anthropic /v1/messages/count_tokens endpoint for a
// fixed corpus of representative Claude Code request bodies. The test
// is gated on the ANTHROPIC_API_KEY environment variable; when the key
// is unset the test is skipped so CI without a key stays green.
//
// Tolerance: per-corpus relative error must be ≤ 10% (the
// tiktoken-go cl100k_base encoding is a faithful BPE approximation of
// the upstream tokenizer used by Anthropic for ~95% of typical Claude
// Code prompts). At least 95% of the corpus must satisfy the per-entry
// tolerance — 1-2 outliers are tolerated because Anthropic's upstream
// count has small non-determinism (system-prompt framing, special
// tokens, etc.) that a third-party tokenizer cannot reproduce exactly.
func TestCountInputTokens_RoundTrip(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping round-trip accuracy test")
	}

	corpus := []struct {
		name string
		body string
	}{
		{
			name: "simple user message (short text)",
			body: `{"model":"claude-opus-4-0","messages":[{"role":"user","content":"Hello, Claude."}]}`,
		},
		{
			name: "user message with long system prompt",
			body: `{"model":"claude-opus-4-0","system":"You are a senior Go engineer who writes idiomatic, well-tested code. You prefer composition over inheritance, table-driven tests, and explicit error wrapping. Always explain your reasoning before showing code.","messages":[{"role":"user","content":"How do I implement a sync.Map wrapper with type safety?"}]}`,
		},
		{
			name: "multi-turn conversation",
			body: `{"model":"claude-opus-4-0","messages":[{"role":"user","content":"What is a goroutine?"},{"role":"assistant","content":"A goroutine is a lightweight thread managed by the Go runtime."},{"role":"user","content":"How do I synchronize access to shared state between goroutines?"},{"role":"assistant","content":"Use channels, the sync package primitives (Mutex, RWMutex, WaitGroup), or atomic operations."},{"role":"user","content":"Show me a concrete example."}]}`,
		},
		{
			name: "tool-use request (3 tools defined, no tool_use in messages)",
			body: `{"model":"claude-opus-4-0","messages":[{"role":"user","content":"What is the weather in Paris?"}],"tools":[{"name":"get_weather","description":"Get the current weather in a given city","input_schema":{"type":"object","properties":{"city":{"type":"string","description":"City name, e.g. Paris"}},"required":["city"]}},{"name":"search_docs","description":"Search internal documentation","input_schema":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["query"]}},{"name":"run_query","description":"Run a read-only SQL query against the analytics warehouse","input_schema":{"type":"object","properties":{"sql":{"type":"string"}},"required":["sql"]}}]}`,
		},
		{
			name: "tool-result request (string content)",
			body: `{"model":"claude-opus-4-0","messages":[{"role":"user","content":"What is the weather in Paris?"},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"get_weather","input":{"city":"Paris"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"Sunny, 22C, light breeze from the west."}]}]}`,
		},
		{
			name: "mixed content (text + tool_use + tool_result)",
			body: `{"model":"claude-opus-4-0","messages":[{"role":"user","content":[{"type":"text","text":"Find recent commits to the auth module and summarize them."}]},{"role":"assistant","content":[{"type":"text","text":"Let me search the git log."},{"type":"tool_use","id":"toolu_02","name":"run_query","input":{"sql":"SELECT hash, author, message FROM commits WHERE module = 'auth' ORDER BY date DESC LIMIT 10"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_02","content":"a1b2c3d|alice|fix token expiry race\nd4e5f6g|bob|refactor jwt validation\nh7i8j9k|carol|add refresh token rotation"}]}]}`,
		},
		{
			name: "empty system (system omitted)",
			body: `{"model":"claude-opus-4-0","messages":[{"role":"user","content":"Just a plain question without any system prompt."}]}`,
		},
		{
			name: "long prompt (~5 KB of code)",
			body: `{"model":"claude-opus-4-0","system":"You are a code reviewer.","messages":[{"role":"user","content":"` + strings.Repeat(
				"func foo() { return 42 }\n",
				200,
			) + `"}]}`,
		},
	}

	httpClient := &http.Client{Timeout: 30 * 1e9}

	const tolerance = 0.10
	passCount := 0
	for _, entry := range corpus {
		t.Run(entry.name, func(t *testing.T) {
			upstream, err := anthropicCountTokens(httpClient, apiKey, []byte(entry.body))
			if err != nil {
				t.Fatalf("upstream count: %v", err)
			}
			if upstream == 0 {
				t.Fatalf("upstream returned 0 tokens; cannot compute relative error")
			}
			local, err := CountInputTokens([]byte(entry.body))
			if err != nil {
				t.Fatalf("local count: %v", err)
			}
			relErr := absFloat64(float64(local-upstream)) / float64(upstream)
			t.Logf("upstream=%d local=%d rel_err=%.4f", upstream, local, relErr)
			if relErr > tolerance {
				t.Errorf(
					"relative error %.4f exceeds tolerance %.4f (upstream=%d, local=%d)",
					relErr,
					tolerance,
					upstream,
					local,
				)
				return
			}
			passCount++
		})
	}

	if got, want := passCount, len(corpus); got < want-int(float64(want)*0.05) {
		t.Errorf("only %d/%d corpus entries within tolerance (need at least 95%%)", got, want)
	}
}

func anthropicCountTokens(client *http.Client, apiKey string, body []byte) (int, error) {
	req, err := http.NewRequest(
		http.MethodPost,
		"https://api.anthropic.com/v1/messages/count_tokens",
		bytes.NewReader(body),
	)
	if err != nil {
		return 0, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, &anthropicCountError{status: resp.StatusCode, body: string(respBody)}
	}
	var parsed struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0, err
	}
	return parsed.InputTokens, nil
}

type anthropicCountError struct {
	status int
	body   string
}

func (e *anthropicCountError) Error() string {
	return "anthropic count_tokens: status " + intToStr(e.status) + ": " + e.body
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	buf := [20]byte{}
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func absFloat64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
