package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/config"
)

func TestProviderPicker_Selection(t *testing.T) {
	p := newProviderPicker()
	if p == nil {
		t.Fatal("newProviderPicker returned nil")
	}

	if p.SelectedProvider() != "" {
		t.Errorf("initial selected = %q, want empty", p.SelectedProvider())
	}

	items := p.list.Items()
	if len(items) != len(config.KnownProviders) {
		t.Errorf("items count = %d, want %d", len(items), len(config.KnownProviders))
	}

	found := false
	for _, item := range items {
		if pi, ok := item.(providerItem); ok && pi.name == "nim" {
			found = true
			if pi.behavior != "openai" {
				t.Errorf("nim behavior = %q, want openai", pi.behavior)
			}
			if pi.apiKeyEnv != "NVIDIA_NIM_API_KEY" {
				t.Errorf("nim apiKeyEnv = %q, want NVIDIA_NIM_API_KEY", pi.apiKeyEnv)
			}
		}
	}
	if !found {
		t.Error("nim not found in picker items")
	}
}

func TestProviderPicker_KeyPressEnterSelects(t *testing.T) {
	p := newProviderPicker()

	_ = p.list.SetItem(0, providerItem{name: "test-provider", behavior: "test"})

	_, done := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !done {
		t.Error("expected done=true after enter")
	}
}

func TestProviderPicker_KeyPressEscCancels(t *testing.T) {
	p := newProviderPicker()

	_, done := p.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !done {
		t.Error("expected done=true after esc")
	}
}

func TestProviderPicker_Navigation(t *testing.T) {
	p := newProviderPicker()

	initialIndex := p.list.Index()
	_, done := p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if done {
		t.Error("expected done=false after down arrow (not a selection)")
	}
	if p.list.Index() != initialIndex+1 {
		t.Errorf("index after down = %d, want %d", p.list.Index(), initialIndex+1)
	}
}

func TestProviderInfo(t *testing.T) {
	tests := []struct {
		name            string
		provider        string
		wantBehavior    string
		wantAPIKeyEnv   string
		wantBaseURL     string
		wantRequiresURL bool
	}{
		{
			name:            "nim",
			provider:        "nim",
			wantBehavior:    "openai",
			wantAPIKeyEnv:   "NVIDIA_NIM_API_KEY",
			wantBaseURL:     "https://integrate.api.nvidia.com/v1/chat/completions",
			wantRequiresURL: false,
		},
		{
			name:            "openai",
			provider:        "openai",
			wantBehavior:    "openai",
			wantAPIKeyEnv:   "",
			wantBaseURL:     "",
			wantRequiresURL: true,
		},
		{
			name:            "anthropic",
			provider:        "anthropic",
			wantBehavior:    "anthropic",
			wantAPIKeyEnv:   "ANTHROPIC_API_KEY",
			wantBaseURL:     "",
			wantRequiresURL: true,
		},
		{
			name:            "mix",
			provider:        "mix",
			wantBehavior:    "mix",
			wantAPIKeyEnv:   "",
			wantBaseURL:     "",
			wantRequiresURL: true,
		},
		{
			name:            "zen",
			provider:        "zen",
			wantBehavior:    "mix",
			wantAPIKeyEnv:   "OPENCODE_API_KEY",
			wantBaseURL:     "",
			wantRequiresURL: true,
		},
		{
			name:            "go",
			provider:        "go",
			wantBehavior:    "mix",
			wantAPIKeyEnv:   "OPENCODE_API_KEY",
			wantBaseURL:     "",
			wantRequiresURL: true,
		},
		{
			name:            "custom",
			provider:        "custom",
			wantBehavior:    "mix",
			wantAPIKeyEnv:   "",
			wantBaseURL:     "",
			wantRequiresURL: true,
		},
		{
			name:            "unknown",
			provider:        "unknown",
			wantBehavior:    "",
			wantAPIKeyEnv:   "",
			wantBaseURL:     "",
			wantRequiresURL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			behavior, apiKeyEnv, baseURL, requiresURL := config.ProviderInfo(tt.provider)
			if behavior != tt.wantBehavior {
				t.Errorf("behavior = %q, want %q", behavior, tt.wantBehavior)
			}
			if apiKeyEnv != tt.wantAPIKeyEnv {
				t.Errorf("apiKeyEnv = %q, want %q", apiKeyEnv, tt.wantAPIKeyEnv)
			}
			if baseURL != tt.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", baseURL, tt.wantBaseURL)
			}
			if requiresURL != tt.wantRequiresURL {
				t.Errorf("requiresURL = %v, want %v", requiresURL, tt.wantRequiresURL)
			}
		})
	}
}
