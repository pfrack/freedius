package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantErr   bool
		errSubstr string
		check     func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid single model",
			yaml: `models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Models) != 1 {
					t.Fatalf("expected 1 model, got %d", len(cfg.Models))
				}
				m, ok := cfg.Models["claude-opus-4"]
				if !ok {
					t.Fatal("expected claude-opus-4 in models")
				}
				if m.Provider != "nim" {
					t.Errorf("provider: got %q, want nim", m.Provider)
				}
				if m.Model != "meta/llama-3.1-70b-instruct" {
					t.Errorf("model: got %q, want meta/llama-3.1-70b-instruct", m.Model)
				}
				if m.BaseURL == "" {
					t.Error("expected nim default base_url to be filled in")
				}
				if m.APIKeyEnv != "NVIDIA_NIM_API_KEY" {
					t.Errorf("api_key_env: got %q, want NVIDIA_NIM_API_KEY", m.APIKeyEnv)
				}
			},
		},
		{
			name: "valid two models",
			yaml: `models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
    base_url: https://example.com/v1/messages
    api_key_env: CUSTOM_API_KEY
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Models) != 2 {
					t.Fatalf("expected 2 models, got %d", len(cfg.Models))
				}
				if _, ok := cfg.Models["claude-opus-4"]; !ok {
					t.Error("missing claude-opus-4")
				}
				sonnet, ok := cfg.Models["claude-sonnet-4"]
				if !ok {
					t.Fatal("missing claude-sonnet-4")
				}
				if sonnet.Provider != "anthropic" {
					t.Errorf("custom should rewrite to anthropic, got %q", sonnet.Provider)
				}
			},
		},
		{
			name:      "empty file",
			yaml:      ``,
			wantErr:   true,
			errSubstr: "contains no model mappings",
		},
		{
			name:      "empty models map",
			yaml:      "models: {}\n",
			wantErr:   true,
			errSubstr: "contains no model mappings",
		},
		{
			name: "malformed YAML",
			yaml: `models:
  claude-opus-4:
    provider: nim
   model: foo
`,
			wantErr:   true,
			errSubstr: "[",
		},
		{
			name: "unknown provider",
			yaml: `models:
  claude-opus-4:
    provider: foo
    model: bar
`,
			wantErr:   true,
			errSubstr: `model "claude-opus-4" uses unknown provider "foo" (known: anthropic, custom, go, mix, nim, openai, zen)`,
		},
		{
			name: "unknown field typo",
			yaml: `models:
  claude-opus-4:
    provder: nim
    model: foo
`,
			wantErr:   true,
			errSubstr: `unknown field "provder"`,
		},
		{
			name: "missing model field",
			yaml: `models:
  claude-opus-4:
    provider: nim
`,
			wantErr:   true,
			errSubstr: `model "claude-opus-4" has no "model" field`,
		},
		{
			name: "missing provider field",
			yaml: `models:
  claude-opus-4:
    model: foo
`,
			wantErr:   true,
			errSubstr: `model "claude-opus-4" has no "provider" field`,
		},
		{
			name: "non-string provider",
			yaml: `models:
  claude-opus-4:
    provider: 42
    model: foo
`,
			wantErr: true,
		},
		{
			name: "model with header-unsafe characters",
			yaml: "models:\n  claude-opus-4:\n    provider: nim\n    model: \"foo\\r\\nX-Injected: bar\"\n",
			wantErr:   true,
			errSubstr: "unsafe \"model\" value",
		},
		{
			name: "openai without base_url",
			yaml: `models:
  claude-sonnet-4:
    provider: openai
    model: gpt-4
    api_key_env: OPENAI_API_KEY
`,
			wantErr:   true,
			errSubstr: `provider=openai but no base_url`,
		},
		{
			name: "anthropic without base_url",
			yaml: `models:
  claude-sonnet-4:
    provider: anthropic
    model: claude-sonnet
    api_key_env: ANTHROPIC_API_KEY
`,
			wantErr:   true,
			errSubstr: `provider=anthropic but no base_url`,
		},
		{
			name: "base_url with invalid scheme",
			yaml: `models:
  claude-sonnet-4:
    provider: openai
    model: gpt-4
    base_url: ftp://example.com/v1/chat/completions
    api_key_env: OPENAI_API_KEY
`,
			wantErr:   true,
			errSubstr: `invalid scheme`,
		},
		{
			name: "api_key_env with newline",
			yaml: "models:\n  claude-opus-4:\n    provider: openai\n    model: gpt-4\n    base_url: https://example.com\n    api_key_env: \"OPENAI\\nKEY\"\n",
			wantErr:   true,
			errSubstr: "api_key_env with invalid characters",
		},
		{
			name: "valid openai with all fields",
			yaml: `models:
  gpt-4:
    provider: openai
    model: gpt-4
    base_url: https://api.openai.com/v1/chat/completions
    api_key_env: OPENAI_API_KEY
`,
			check: func(t *testing.T, cfg *Config) {
				m, ok := cfg.Models["gpt-4"]
				if !ok {
					t.Fatal("missing gpt-4")
				}
				if m.BaseURL != "https://api.openai.com/v1/chat/completions" {
					t.Errorf("base_url: got %q", m.BaseURL)
				}
			},
		},
		{
			name: "valid custom alias rewrite",
			yaml: `models:
  my-shim:
    provider: custom
    model: shim-v1
    base_url: https://example.com/v1/messages
    api_key_env: CUSTOM_KEY
`,
			check: func(t *testing.T, cfg *Config) {
				m, ok := cfg.Models["my-shim"]
				if !ok {
					t.Fatal("missing my-shim")
				}
				if m.Provider != "anthropic" {
					t.Errorf("custom should rewrite to anthropic, got %q", m.Provider)
				}
			},
		},
		{
			name: "valid mappings block",
			yaml: `mappings:
  opus:
    provider: nim
    model: meta/llama-3.1-70b-instruct
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Mappings) != 1 {
					t.Fatalf("expected 1 mapping, got %d", len(cfg.Mappings))
				}
				if _, ok := cfg.Mappings["opus"]; !ok {
					t.Fatal("missing opus mapping")
				}
			},
		},
		{
			name: "mappings with openai no base_url",
			yaml: `mappings:
  opus:
    provider: openai
    model: gpt-4
    api_key_env: OPENAI_API_KEY
`,
			wantErr:   true,
			errSubstr: `provider=openai but no base_url`,
		},
		{
			name: "mappings with invalid scheme",
			yaml: `mappings:
  opus:
    provider: openai
    model: gpt-4
    base_url: ftp://example.com
    api_key_env: OPENAI_API_KEY
`,
			wantErr:   true,
			errSubstr: `invalid scheme`,
		},
		{
			name: "mappings with api_key_env containing equals",
			yaml: `mappings:
  opus:
    provider: openai
    model: gpt-4
    base_url: https://x
    api_key_env: "KEY=VALUE"
`,
			wantErr:   true,
			errSubstr: "api_key_env with invalid characters",
		},
		{
			name: "valid zen model",
			yaml: `models:
  test:
    provider: zen
    model: test-model
    base_url: https://opencode.ai/zen/v1/messages
`,
			check: func(t *testing.T, cfg *Config) {
				m, ok := cfg.Models["test"]
				if !ok {
					t.Fatal("missing test model")
				}
				if m.Provider != "mix" {
					t.Errorf("zen should rewrite to mix, got %q", m.Provider)
				}
				if m.BaseURL != "https://opencode.ai/zen/v1/messages" {
					t.Errorf("base_url: got %q", m.BaseURL)
				}
				if m.APIKeyEnv != "OPENCODE_API_KEY" {
					t.Errorf("api_key_env: got %q, want OPENCODE_API_KEY", m.APIKeyEnv)
				}
			},
		},
		{
			name: "valid go model",
			yaml: `models:
  test:
    provider: go
    model: test-model
    base_url: https://opencode.ai/zen/go/v1/messages
`,
			check: func(t *testing.T, cfg *Config) {
				m, ok := cfg.Models["test"]
				if !ok {
					t.Fatal("missing test model")
				}
				if m.Provider != "mix" {
					t.Errorf("go should rewrite to mix, got %q", m.Provider)
				}
				if m.BaseURL != "https://opencode.ai/zen/go/v1/messages" {
					t.Errorf("base_url: got %q", m.BaseURL)
				}
				if m.APIKeyEnv != "OPENCODE_API_KEY" {
					t.Errorf("api_key_env: got %q, want OPENCODE_API_KEY", m.APIKeyEnv)
				}
			},
		},
		{
			name: "valid mix model",
			yaml: `models:
  test:
    provider: mix
    model: test-model
    base_url: https://example.com/v1/chat/completions
    api_key_env: MIX_API_KEY
`,
			check: func(t *testing.T, cfg *Config) {
				m, ok := cfg.Models["test"]
				if !ok {
					t.Fatal("missing test model")
				}
				if m.Provider != "mix" {
					t.Errorf("provider: got %q, want mix", m.Provider)
				}
			},
		},
		{
			name: "zen without base_url",
			yaml: `models:
  test:
    provider: zen
    model: test-model
`,
			wantErr:   true,
			errSubstr: `provider=mix but no base_url`,
		},
		{
			name: "go without base_url",
			yaml: `models:
  test:
    provider: go
    model: test-model
`,
			wantErr:   true,
			errSubstr: `provider=mix but no base_url`,
		},
		{
			name: "mix without base_url",
			yaml: `models:
  test:
    provider: mix
    model: test-model
    api_key_env: MIX_API_KEY
`,
			wantErr:   true,
			errSubstr: `provider=mix but no base_url`,
		},
		{
			name: "models empty but mappings non-empty",
			yaml: `mappings:
  opus:
    provider: nim
    model: x
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Models) != 0 {
					t.Errorf("expected 0 models, got %d", len(cfg.Models))
				}
				if len(cfg.Mappings) != 1 {
					t.Errorf("expected 1 mapping, got %d", len(cfg.Mappings))
				}
			},
		},
		{
			name:      "empty mappings block alone is not enough",
			yaml:      "mappings: {}\n",
			wantErr:   true,
			errSubstr: "contains no model mappings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "freedius.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; cfg=%+v", cfg)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.yaml")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error does not wrap os.ErrNotExist: %v", err)
	}
	if !strings.Contains(err.Error(), "config file not found at") {
		t.Errorf("error does not contain expected message: %v", err)
	}
}

func TestKnownProviders(t *testing.T) {
	expected := []string{"nim", "zen", "go", "custom", "openai", "anthropic", "mix"}
	if len(KnownProviders) != len(expected) {
		t.Errorf("KnownProviders has %d entries, want %d", len(KnownProviders), len(expected))
	}
	for _, e := range expected {
		if _, ok := KnownProviders[e]; !ok {
			t.Errorf("KnownProviders missing %q", e)
		}
	}
}

func TestProviderEnvVar(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"nim", "nim", "NVIDIA_NIM_API_KEY"},
		{"zen", "zen", "OPENCODE_API_KEY"},
		{"go", "go", "OPENCODE_API_KEY"},
		{"openai has no default", "openai", ""},
		{"anthropic has no default", "anthropic", ""},
		{"custom has no default", "custom", ""},
		{"mix has no default", "mix", ""},
		{"unknown", "unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProviderEnvVar(tt.in)
			if got != tt.want {
				t.Errorf("ProviderEnvVar(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUsesProvider(t *testing.T) {
	cfg := &Config{
		Models: map[string]Model{
			"a": {Provider: "nim"},
			"b": {Provider: "openai"},
		},
		Mappings: map[string]Model{
			"opus": {Provider: "anthropic"},
		},
	}
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"nim in models", "nim", true},
		{"openai in models", "openai", true},
		{"anthropic in mappings", "anthropic", true},
		{"zen not used", "zen", false},
		{"go not used", "go", false},
		{"custom not used (post-rewrite)", "custom", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.UsesProvider(tt.in); got != tt.want {
				t.Errorf("UsesProvider(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
