package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/pfrack/freedius/config"
)

func TestResolveUserAgent(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
		want     string
	}{
		{"claude-cli forwarded", "claude-cli/2.0.0 (external, cli)", "claude-cli/2.0.0 (external, cli)"},
		{"browser forwarded", "Mozilla/5.0 (X11; Linux x86_64)", "Mozilla/5.0 (X11; Linux x86_64)"},
		{"empty falls back", "", fallbackUserAgent},
		{"go default replaced", goDefaultUserAgent, fallbackUserAgent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			if tt.incoming != "" {
				r.Header.Set("User-Agent", tt.incoming)
			}
			if got := resolveUserAgent(r); got != tt.want {
				t.Errorf("resolveUserAgent = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("nil request", func(t *testing.T) {
		if got := resolveUserAgent(nil); got != fallbackUserAgent {
			t.Errorf("resolveUserAgent(nil) = %q, want %q", got, fallbackUserAgent)
		}
	})
}

// TestOpenAIAdapter_ForwardsUserAgent proves the adapter sends the incoming
// client's UA upstream (never Go's default), which is what unblocks
// Cloudflare-protected upstreams like Nous.
func TestOpenAIAdapter_ForwardsUserAgent(t *testing.T) {
	t.Setenv("NOUSRESEARCH_API_KEY", "sk-test")
	var mu sync.Mutex
	var capturedUA string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedUA = r.Header.Get("User-Agent")
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	a := newOpenAIAdapterForTest(t)

	for _, tt := range []struct {
		name     string
		incoming string
		wantUA   string
	}{
		{"client UA forwarded", "claude-cli/2.0.0 (external, cli)", "claude-cli/2.0.0 (external, cli)"},
		{"missing UA uses fallback", "", fallbackUserAgent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			capturedUA = ""
			mu.Unlock()

			rec := httptest.NewRecorder()
			body := []byte(`{"model":"m","max_tokens":10,"stream":false,"messages":[{"role":"user","content":"hi"}]}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			if tt.incoming != "" {
				req.Header.Set("User-Agent", tt.incoming)
			}
			err := a.Handle(
				rec,
				req,
				config.Provider{
					Behavior:         "openai",
					DefaultBaseURL:   upstream.URL + "/v1/chat/completions",
					DefaultAPIKeyEnv: "NOUSRESEARCH_API_KEY",
				},
				config.Mapping{ProviderName: "nous", ModelString: "m"},
				body,
			)
			if err != nil {
				t.Fatalf("Handle returned err: %v", err)
			}
			mu.Lock()
			got := capturedUA
			mu.Unlock()
			if got != tt.wantUA {
				t.Errorf("upstream User-Agent = %q, want %q", got, tt.wantUA)
			}
			if got == goDefaultUserAgent {
				t.Errorf("upstream received Go default UA %q — this is blocked by Cloudflare", got)
			}
		})
	}
}
