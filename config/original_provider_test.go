package config

import "testing"

func TestApplyEntryDefaults_OriginalProviderSetBeforeRewrite(t *testing.T) {
	tests := []struct {
		name     string
		input    Model
		wantOrig string
		wantProv string
	}{
		{
			name:     "nim keeps both",
			input:    Model{Provider: "nim", Model: "x"},
			wantOrig: "nim",
			wantProv: "nim",
		},
		{
			name: "custom rewrites Provider but preserves OriginalProvider",
			input: Model{
				Provider:  "custom",
				Model:     "x",
				BaseURL:   "https://x",
				APIKeyEnv: "CUSTOM_KEY",
			},
			wantOrig: "custom",
			wantProv: "anthropic",
		},
		{
			name:     "zen rewrites Provider but preserves OriginalProvider",
			input:    Model{Provider: "zen", Model: "x"},
			wantOrig: "zen",
			wantProv: "mix",
		},
		{
			name:     "go rewrites Provider but preserves OriginalProvider",
			input:    Model{Provider: "go", Model: "x"},
			wantOrig: "go",
			wantProv: "mix",
		},
		{
			name:     "openai keeps both (no rewrite)",
			input:    Model{Provider: "openai", Model: "x", BaseURL: "https://x"},
			wantOrig: "openai",
			wantProv: "openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyEntryDefaults(tt.input)
			if got.OriginalProvider != tt.wantOrig {
				t.Errorf("OriginalProvider: got %q, want %q", got.OriginalProvider, tt.wantOrig)
			}
			if got.Provider != tt.wantProv {
				t.Errorf("Provider: got %q, want %q", got.Provider, tt.wantProv)
			}
		})
	}
}

func TestApplyEntryDefaults_DoesNotOverwriteExistingOriginalProvider(t *testing.T) {
	// Once OriginalProvider is set (e.g., from a previous applyDefaults call),
	// subsequent calls must NOT replace it with the rewritten Provider.
	first := applyEntryDefaults(
		Model{Provider: "custom", Model: "x", BaseURL: "https://x", APIKeyEnv: "K"},
	)
	if first.OriginalProvider != "custom" || first.Provider != "anthropic" {
		t.Fatalf(
			"first pass: got orig=%q prov=%q, want orig=custom prov=anthropic",
			first.OriginalProvider,
			first.Provider,
		)
	}
	second := applyEntryDefaults(first)
	if second.OriginalProvider != "custom" || second.Provider != "anthropic" {
		t.Errorf(
			"second pass: got orig=%q prov=%q, want orig=custom prov=anthropic (stable)",
			second.OriginalProvider,
			second.Provider,
		)
	}
}

func TestLoad_SetsOriginalProviderThroughPipeline(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/freedius.yaml"
	yaml := `models:
  opus: { provider: custom, model: x, base_url: https://x/v1/messages, api_key_env: CUSTOM_KEY }
  haiku: { provider: zen, model: x, base_url: https://x/v1/messages }
`
	if err := writeFile(path, []byte(yaml)); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	opus, ok := cfg.Models["opus"]
	if !ok {
		t.Fatal("missing opus model")
	}
	if opus.OriginalProvider != "custom" {
		t.Errorf("opus OriginalProvider: got %q, want custom", opus.OriginalProvider)
	}
	if opus.Provider != "anthropic" {
		t.Errorf("opus Provider: got %q, want anthropic (post-rewrite)", opus.Provider)
	}
	haiku, ok := cfg.Models["haiku"]
	if !ok {
		t.Fatal("missing haiku model")
	}
	if haiku.OriginalProvider != "zen" {
		t.Errorf("haiku OriginalProvider: got %q, want zen", haiku.OriginalProvider)
	}
	if haiku.Provider != "mix" {
		t.Errorf("haiku Provider: got %q, want mix (post-rewrite)", haiku.Provider)
	}
}
