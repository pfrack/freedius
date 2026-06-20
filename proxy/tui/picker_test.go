package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/config"
)

func TestProviderPicker_Selection(t *testing.T) {
	providers := map[string]config.Provider{
		"nim": {
			Behavior:         "openai",
			DefaultBaseURL:   "https://x/v1/chat/completions",
			DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY",
		},
		"zen":  {Behavior: "mix"},
		"anth": {Behavior: "anthropic"},
	}
	p := newProviderPicker(sortedConfiguredProviderNames(providers), providers)
	if p == nil {
		t.Fatal("newProviderPicker returned nil")
	}

	if p.SelectedProvider() != "" {
		t.Errorf("initial selected = %q, want empty", p.SelectedProvider())
	}

	items := p.list.Items()
	if len(items) != len(providers) {
		t.Errorf("items count = %d, want %d", len(items), len(providers))
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
	providers := map[string]config.Provider{"nim": {Behavior: "openai"}}
	p := newProviderPicker(sortedConfiguredProviderNames(providers), providers)

	_ = p.list.SetItem(0, providerItem{name: "test-provider", behavior: "test"})

	_, done := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !done {
		t.Error("expected done=true after enter")
	}
}

func TestProviderPicker_KeyPressEscCancels(t *testing.T) {
	providers := map[string]config.Provider{"nim": {Behavior: "openai"}}
	p := newProviderPicker(sortedConfiguredProviderNames(providers), providers)

	_, done := p.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !done {
		t.Error("expected done=true after esc")
	}
}

func TestProviderPicker_Navigation(t *testing.T) {
	providers := map[string]config.Provider{
		"nim":  {Behavior: "openai"},
		"zen":  {Behavior: "mix"},
		"anth": {Behavior: "anthropic"},
	}
	p := newProviderPicker(sortedConfiguredProviderNames(providers), providers)

	initialIndex := p.list.Index()
	_, done := p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if done {
		t.Error("expected done=false after down arrow (not a selection)")
	}
	if p.list.Index() != initialIndex+1 {
		t.Errorf("index after down = %d, want %d", p.list.Index(), initialIndex+1)
	}
}

func TestBehaviorPicker_Selection(t *testing.T) {
	p := newBehaviorPicker()
	if p == nil {
		t.Fatal("newBehaviorPicker returned nil")
	}

	items := p.list.Items()
	if len(items) != 3 {
		t.Errorf("behavior picker items = %d, want 3", len(items))
	}

	wantBehaviors := map[string]bool{"openai": false, "anthropic": false, "mix": false}
	for _, item := range items {
		pi, ok := item.(providerItem)
		if !ok {
			t.Errorf("unexpected item type: %T", item)
			continue
		}
		if _, expected := wantBehaviors[pi.name]; !expected {
			t.Errorf("unexpected behavior in picker: %q", pi.name)
		}
		wantBehaviors[pi.name] = true
	}
	for name, seen := range wantBehaviors {
		if !seen {
			t.Errorf("expected behavior %q in picker, missing", name)
		}
	}
}
