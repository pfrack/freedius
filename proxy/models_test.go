package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/pfrack/freedius/config"
)

func TestDeriveModelsURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
		wantErr bool
	}{
		{
			name:    "groq",
			baseURL: "https://api.groq.com/openai/v1/chat/completions",
			want:    "https://api.groq.com/openai/v1/models",
		},
		{
			name:    "deepseek no /v1",
			baseURL: "https://api.deepseek.com/chat/completions",
			want:    "https://api.deepseek.com/models",
		},
		{
			name:    "anthropic",
			baseURL: "https://api.anthropic.com/v1/messages",
			want:    "https://api.anthropic.com/v1/models",
		},
		{
			name:    "zen mix",
			baseURL: "https://opencode.ai/zen/v1/messages",
			want:    "https://opencode.ai/zen/v1/models",
		},
		{
			name:    "go mix",
			baseURL: "https://opencode.ai/zen/go/v1/chat/completions",
			want:    "https://opencode.ai/zen/go/v1/models",
		},
		{
			name:    "ollama",
			baseURL: "http://localhost:11434/v1/chat/completions",
			want:    "http://localhost:11434/v1/models",
		},
		{
			name:    "no recognized suffix",
			baseURL: "https://api.example.com/v1/embeddings",
			want:    "https://api.example.com/v1/embeddings/models",
		},
		{
			name:    "root path no suffix",
			baseURL: "https://api.example.com/",
			want:    "https://api.example.com/models",
		},
		{
			name:    "path with only /models already",
			baseURL: "https://api.example.com/v1/models",
			want:    "https://api.example.com/v1/models/models",
		},
		{
			name:    "invalid URL",
			baseURL: "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deriveModelsURL(tt.baseURL)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("deriveModelsURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestModelsCache(t *testing.T) {
	t.Run("set get round trip", func(t *testing.T) {
		c := NewModelsCache()
		models := []ModelView{
			{ID: "gpt-4o", DisplayName: "gpt-4o"},
			{ID: "gpt-4o-mini", DisplayName: "gpt-4o-mini"},
		}

		c.Set("openai", models, nil)

		got, fetchedAt, err := c.Get("openai")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 models, got %d", len(got))
		}
		if got[0].ID != "gpt-4o" {
			t.Errorf("model[0].ID = %q, want %q", got[0].ID, "gpt-4o")
		}
		if got[1].ID != "gpt-4o-mini" {
			t.Errorf("model[1].ID = %q, want %q", got[1].ID, "gpt-4o-mini")
		}
		if fetchedAt.IsZero() {
			t.Error("FetchedAt should be non-zero")
		}
	})

	t.Run("miss returns zero values", func(t *testing.T) {
		c := NewModelsCache()
		models, fetchedAt, err := c.Get("nonexistent")
		if err != nil {
			t.Errorf("expected nil error on miss, got %v", err)
		}
		if models != nil {
			t.Errorf("expected nil models on miss, got %v", models)
		}
		if !fetchedAt.IsZero() {
			t.Errorf("expected zero time on miss, got %v", fetchedAt)
		}
	})

	t.Run("error entry returns models", func(t *testing.T) {
		c := NewModelsCache()
		c.Set("broken", nil, errors.New("test error"))

		models, _, err := c.Get("broken")
		if err == nil {
			t.Fatal("expected error")
		}
		if models != nil {
			t.Errorf("expected nil models on error entry, got %v", models)
		}
	})
}

func TestModelsCacheConcurrent(t *testing.T) {
	t.Helper()
	c := NewModelsCache()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			c.Set("provider", []ModelView{{ID: "m"}}, nil)
			_, _, _ = c.Get("provider")
		}(i)
	}
	wg.Wait()
}

func TestFetchModels(t *testing.T) {
	t.Run("openai shape", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data": []map[string]any{
					{"id": "gpt-4o", "object": "model", "created": 1715367049, "owned_by": "openai"},
					{"id": "gpt-4o-mini", "object": "model", "created": 1715367049, "owned_by": "openai"},
				},
			})
		}))
		defer srv.Close()

		models, err := FetchModels(t.Context(), config.Provider{
			Behavior:       "openai",
			DefaultBaseURL: srv.URL + "/v1/chat/completions",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(models))
		}
		if models[0].ID != "gpt-4o" {
			t.Errorf("model[0].ID = %q, want %q", models[0].ID, "gpt-4o")
		}
		if models[0].DisplayName != "gpt-4o" {
			t.Errorf("model[0].DisplayName = %q, want %q (fallback to ID)", models[0].DisplayName, "gpt-4o")
		}
	})

	t.Run("anthropic shape", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":           "claude-opus-4-6",
						"type":         "model",
						"display_name": "Claude Opus 4.6",
						"created_at":   "2026-02-04T00:00:00Z",
					},
				},
				"has_more": false,
			})
		}))
		defer srv.Close()

		models, err := FetchModels(t.Context(), config.Provider{
			Behavior:       "anthropic",
			DefaultBaseURL: srv.URL + "/v1/messages",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
		if models[0].ID != "claude-opus-4-6" {
			t.Errorf("model[0].ID = %q, want %q", models[0].ID, "claude-opus-4-6")
		}
		if models[0].DisplayName != "Claude Opus 4.6" {
			t.Errorf("model[0].DisplayName = %q, want %q", models[0].DisplayName, "Claude Opus 4.6")
		}
	})

	t.Run("error response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
		}))
		defer srv.Close()

		_, err := FetchModels(t.Context(), config.Provider{
			Behavior:       "openai",
			DefaultBaseURL: srv.URL + "/v1/chat/completions",
		})
		if err == nil {
			t.Fatal("expected error for 401 response")
		}
	})

	t.Run("empty data array", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": []}`))
		}))
		defer srv.Close()

		models, err := FetchModels(t.Context(), config.Provider{
			Behavior:       "openai",
			DefaultBaseURL: srv.URL + "/v1/chat/completions",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(models) != 0 {
			t.Errorf("expected 0 models, got %d", len(models))
		}
	})

	t.Run("mix protocol via Protocol field", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": [{"id": "zen-model", "display_name": "Zen Model"}]}`))
		}))
		defer srv.Close()

		models, err := FetchModels(t.Context(), config.Provider{
			Behavior:       "mix",
			Protocol:       "anthropic",
			DefaultBaseURL: srv.URL + "/v1/messages",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
	})

	t.Run("mix protocol via URL sniffing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []map[string]any{{"id": "openai-model"}},
			})
		}))
		defer srv.Close()

		models, err := FetchModels(t.Context(), config.Provider{
			Behavior:       "mix",
			DefaultBaseURL: srv.URL + "/v1/chat/completions",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
	})

	t.Run("unreachable server", func(t *testing.T) {
		_, err := FetchModels(t.Context(), config.Provider{
			Behavior:       "openai",
			DefaultBaseURL: "http://127.0.0.1:1/chat/completions",
		})
		if err == nil {
			t.Fatal("expected error for unreachable server")
		}
	})

	t.Run("no api key env set, request proceeds without auth", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
				t.Error("expected no auth headers when APIKeyEnv is empty")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": [{"id": "local-model"}]}`))
		}))
		defer srv.Close()

		_, err := FetchModels(t.Context(), config.Provider{
			Behavior:       "openai",
			DefaultBaseURL: srv.URL + "/v1/chat/completions",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("api key not set returns empty silently", func(t *testing.T) {
		models, err := FetchModels(t.Context(), config.Provider{
			Behavior:         "openai",
			DefaultAPIKeyEnv: "NONEXISTENT_ENV_VAR_FOR_TEST",
			DefaultBaseURL:   "http://127.0.0.1:1/chat/completions",
		})
		if err != nil {
			t.Errorf("expected nil error when API key is not set, got %v", err)
		}
		if models != nil {
			t.Errorf("expected nil models when API key is not set, got %v", models)
		}
	})
}

func TestDeriveModelsURL_NoV1Segment(t *testing.T) {
	// deepseek edge case: no /v1 in path
	got, err := deriveModelsURL("https://api.deepseek.com/chat/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://api.deepseek.com/models"
	if got != want {
		t.Errorf("deriveModelsURL() = %q, want %q", got, want)
	}
}

func TestResolveMixProtocol(t *testing.T) {
	t.Run("explicit protocol wins", func(t *testing.T) {
		got := resolveMixProtocol("https://example.com/v1/messages", "openai")
		if got != "openai" {
			t.Errorf("expected openai, got %s", got)
		}
	})

	t.Run("url sniffing anthropic", func(t *testing.T) {
		got := resolveMixProtocol("https://example.com/v1/messages", "")
		if got != "anthropic" {
			t.Errorf("expected anthropic, got %s", got)
		}
	})

	t.Run("url sniffing default to openai", func(t *testing.T) {
		got := resolveMixProtocol("https://example.com/v1/chat/completions", "")
		if got != "openai" {
			t.Errorf("expected openai, got %s", got)
		}
	})
}

func TestDeriveModelsURL_MessagesSuffix(t *testing.T) {
	got, err := deriveModelsURL("https://api.anthropic.com/v1/messages")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://api.anthropic.com/v1/models"
	if got != want {
		t.Errorf("deriveModelsURL() = %q, want %q", got, want)
	}
}
