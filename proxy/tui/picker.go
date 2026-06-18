package tui

import (
	"charm.land/bubbles/v2/list"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/config"
)

type providerItem struct {
	name        string
	behavior    string
	apiKeyEnv   string
	baseURL     string
	requiresURL bool
}

func (i providerItem) FilterValue() string { return i.name }

type providerPicker struct {
	list     list.Model
	selected string
}

func newProviderPicker() *providerPicker {
	items := make([]list.Item, 0, len(config.KnownProviders))
	sorted := sortedProviderNames()
	for _, name := range sorted {
		behavior, apiKeyEnv, baseURL, requiresURL := config.ProviderInfo(name)
		items = append(items, providerItem{
			name:        name,
			behavior:    behavior,
			apiKeyEnv:   apiKeyEnv,
			baseURL:     baseURL,
			requiresURL: requiresURL,
		})
	}

	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, 40, 14)
	l.Title = "Select Provider"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)

	return &providerPicker{
		list: l,
	}
}

func (p *providerPicker) Update(msg tea.Msg) (tea.Cmd, bool) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if i, ok := p.list.SelectedItem().(providerItem); ok {
				p.selected = i.name
			}
			return nil, true
		case "esc":
			return nil, true
		}
	}
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return cmd, false
}

func (p *providerPicker) View() string {
	return windowStyle.Render(p.list.View())
}

func (p *providerPicker) SelectedProvider() string {
	return p.selected
}

func sortedProviderNames() []string {
	sorted := make([]string, 0, len(config.KnownProviders))
	for _, n := range []string{"anthropic", "custom", "go", "mix", "nim", "openai", "zen"} {
		if _, ok := config.KnownProviders[n]; ok {
			sorted = append(sorted, n)
		}
	}
	return sorted
}
