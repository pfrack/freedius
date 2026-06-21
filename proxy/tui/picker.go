package tui

import (
	"sort"

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
	styles   Styles
}

// newProviderPicker builds a picker for selecting among the names of providers
// the user has configured in d.config.Providers. Pass the sorted names list.
func newProviderPicker(names []string, providers map[string]config.Provider, styles Styles) *providerPicker {
	items := make([]list.Item, 0, len(names))
	for _, name := range names {
		p := providers[name]
		items = append(items, providerItem{
			name:        name,
			behavior:    p.Behavior,
			apiKeyEnv:   p.DefaultAPIKeyEnv,
			baseURL:     p.DefaultBaseURL,
			requiresURL: p.RequireBaseURL,
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
		list:   l,
		styles: styles,
	}
}

// newBehaviorPicker builds a picker for the Behavior field of a provider form.
// The list is fixed to the three valid behavior values.
func newBehaviorPicker(styles Styles) *providerPicker {
	behaviors := []struct {
		name string
	}{
		{name: "openai"},
		{name: "anthropic"},
		{name: "mix"},
	}
	items := make([]list.Item, 0, len(behaviors))
	for _, b := range behaviors {
		items = append(items, providerItem{name: b.name, behavior: b.name})
	}
	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, 40, len(behaviors)+2)
	l.Title = "Select Behavior"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	return &providerPicker{list: l, styles: styles}
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
	return p.styles.WindowStyle.Render(p.list.View())
}

func (p *providerPicker) SelectedProvider() string {
	return p.selected
}

// sortedConfiguredProviderNames returns the names of d.config.Providers
// sorted alphabetically. Used by the mapping form's provider picker.
func sortedConfiguredProviderNames(providers map[string]config.Provider) []string {
	names := make([]string, 0, len(providers))
	for n := range providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
