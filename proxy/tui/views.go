package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

func renderTabs(active int, width int, level LogFilter, styles Styles) string {
	tabs := []string{
		fmt.Sprintf("[1] Log [%s]", level.Label),
		"[2] Providers",
		"[3] Config (j/k=scroll Enter=edit a=+map p=+prov d=del)",
	}
	styled := make([]string, len(tabs))
	for i, t := range tabs {
		if i == active {
			styled[i] = styles.ActiveTabStyle.Render(t)
		} else {
			styled[i] = styles.InactiveTabStyle.Render(t)
		}
	}
	joined := lipgloss.JoinHorizontal(lipgloss.Top, styled...)
	return styles.TabBarStyle.Width(max(width-2, 0)).Render(joined)
}

func renderLogTab(entries []proxy.LogEntry, _ int, height, scroll int, filter LogFilter) string {
	if len(entries) == 0 {
		return "No log entries yet..."
	}

	available := height - 4
	if available < 0 {
		available = 0
	}
	start := 0
	if len(entries) > available {
		start = len(entries) - available - scroll
		if start < 0 {
			start = 0
		}
	}
	end := start + available
	if end > len(entries) {
		end = len(entries)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		e := entries[i]
		if !filter.Matches(e.Level) {
			continue
		}
		b.WriteString(e.Line + "\n")
	}
	return b.String()
}

func renderProvidersTab(cfg *config.Config, width, height, scroll int, styles Styles) string {
	var b strings.Builder
	b.WriteString(styles.WindowStyle.Render("Provider Configuration") + "\n\n")

	header := styles.ProviderTableHeaderStyle.Render(
		fmt.Sprintf(
			"%-14s %-10s %-30s %-6s",
			"Provider", "Behavior", "Base URL", "Mappings",
		),
	)
	b.WriteString(header + "\n")
	b.WriteString(styles.SeparatorStyle.Render(strings.Repeat("─", max(width-4, 0))) + "\n")

	available := height - 4
	if available < 0 {
		available = 0
	}

	providers := collectProvidersFromConfig(cfg)
	start := 0
	if len(providers) > available {
		start = len(providers) - available - scroll
		if start < 0 {
			start = 0
		}
	}
	end := start + available
	if end > len(providers) {
		end = len(providers)
	}

	for i := start; i < end; i++ {
		p := providers[i]
		line := fmt.Sprintf(
			"%-14s %-10s %-30s %-6d",
			truncate(p.name, 14),
			truncate(p.behavior, 10),
			truncate(p.baseURL, 30),
			p.mappingCount,
		)
		b.WriteString(line + "\n")
	}
	if len(providers) == 0 {
		b.WriteString("(no providers configured)\n")
	}
	return b.String()
}

func renderConfigTab(cfg *config.Config, cursor int, width, height int, styles Styles) string {
	var b strings.Builder
	b.WriteString(styles.WindowStyle.Render("Configuration") + "\n\n")
	b.WriteString(styles.SeparatorStyle.Render(strings.Repeat("─", max(width-4, 0))) + "\n")

	all := collectAllEntries(cfg)
	// Each entry occupies ~6 lines (label + 4 fields + blank) for providers
	// and ~4 lines (label + 2 fields + blank) for mappings. We don't try to
	// measure precisely; instead we use a conservative per-entry height and
	// clamp the visible window. The cursor auto-scrolls into view.
	const approxEntryLines = 6
	available := height - 3
	if available < 0 {
		available = 0
	}
	visibleEntries := available / approxEntryLines
	if visibleEntries < 1 {
		visibleEntries = 1
	}
	if visibleEntries > len(all) {
		visibleEntries = len(all)
	}

	// Center the cursor in the visible window.
	half := visibleEntries / 2
	start := cursor - half
	if start < 0 {
		start = 0
	}
	end := start + visibleEntries
	if end > len(all) {
		end = len(all)
		start = end - visibleEntries
		if start < 0 {
			start = 0
		}
	}

	for i := start; i < end; i++ {
		entry := all[i]
		nameStyle := styles.ConfigKeyStyle
		if i == cursor {
			nameStyle = styles.ActiveTabStyle
		}
		label := nameStyle.Render(entry.name)
		fmt.Fprintf(&b, "%s (%s):\n", label, entry.kind)
		if entry.kind == "provider" {
			provider := entry.provider
			fmt.Fprintf(&b, "  behavior: %s\n", styles.ConfigValueStyle.Render(provider.Behavior))
			if provider.DefaultBaseURL != "" {
				fmt.Fprintf(
					&b,
					"  base_url: %s\n",
					styles.ConfigValueStyle.Render(provider.DefaultBaseURL),
				)
			}
			if provider.DefaultAPIKeyEnv != "" {
				fmt.Fprintf(
					&b,
					"  api_key:  %s\n",
					styles.ConfigValueStyle.Render(provider.DefaultAPIKeyEnv),
				)
			}
			if provider.AnthropicVersion != "" {
				fmt.Fprintf(
					&b,
					"  api_ver:  %s\n",
					styles.ConfigValueStyle.Render(provider.AnthropicVersion),
				)
			}
		} else {
			mapping := entry.mapping
			fmt.Fprintf(&b, "  provider_name: %s\n", styles.ConfigValueStyle.Render(mapping.ProviderName))
			fmt.Fprintf(&b, "  model_string:  %s\n", styles.ConfigValueStyle.Render(mapping.ModelString))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderStatsBar(stats statsData, width int, styles Styles) string {
	uptime := time.Since(stats.startTime).Round(time.Second).String()
	total := stats.totalRequests
	errors := stats.errorCount
	errRate := "0.0%"
	if total > 0 {
		errRate = fmt.Sprintf("%.1f%%", float64(errors)/float64(total)*100)
	}
	line := fmt.Sprintf(
		" uptime: %s │ requests: %d │ errors: %d │ error rate: %s ",
		uptime, total, errors, errRate,
	)
	if stats.message != "" {
		line = line + "│ " + stats.message + " "
	}
	if len(line) < width {
		line += strings.Repeat(" ", width-len(line))
	}
	if len(line) > width {
		line = line[:width]
	}
	return styles.StatsBarStyle.Render(line)
}

type providerInfo struct {
	name         string
	behavior     string
	baseURL      string
	mappingCount int
}

func collectProvidersFromConfig(cfg *config.Config) []providerInfo {
	// Snapshot the maps so we don't race with the dispatcher's write lock.
	providers := cfg.ProvidersSnapshot()
	mappings := cfg.MappingsSnapshot()
	result := make([]providerInfo, 0, len(providers))
	for name, p := range providers {
		mappingCount := 0
		for _, m := range mappings {
			if m.ProviderName == name {
				mappingCount++
			}
		}
		result = append(result, providerInfo{
			name:         name,
			behavior:     p.Behavior,
			baseURL:      p.DefaultBaseURL,
			mappingCount: mappingCount,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].name < result[j].name
	})
	return result
}

// configEntry is one row in the config tab. Exactly one of provider or mapping
// is set, identified by kind.
type configEntry struct {
	name     string
	kind     string // "provider" or "mapping"
	provider config.Provider
	mapping  config.Mapping
}

func collectAllEntries(cfg *config.Config) []configEntry {
	// Snapshot the maps so we don't race with the dispatcher's write lock.
	providers := cfg.ProvidersSnapshot()
	mappings := cfg.MappingsSnapshot()
	var entries []configEntry
	for name, p := range providers {
		entries = append(entries, configEntry{name: name, kind: "provider", provider: p})
	}
	for name, m := range mappings {
		entries = append(entries, configEntry{name: name, kind: "mapping", mapping: m})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].kind != entries[j].kind {
			// providers first, mappings second
			return entries[i].kind > entries[j].kind
		}
		return entries[i].name < entries[j].name
	})
	return entries
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// fieldLabelsForMode returns the ordered list of form-field labels for the
// current form mode (provider or mapping).
func fieldLabelsForMode(mode int) []string {
	switch mode {
	case formEditProvider, formAddProvider:
		return []string{
			"name",
			"behavior",
			"base_url",
			"api_key_env",
			"anthropic_version",
		}
	case formEditMapping, formAddMapping:
		return []string{
			"name",
			"provider",
			"model",
		}
	default:
		return nil
	}
}

func renderForm(d *Dashboard, width, _ int) string {
	if d.formMode == formDeleteConfirm {
		return renderDeleteConfirm(d, width)
	}

	var b strings.Builder
	b.WriteString(d.styles.WindowStyle.Render("Edit Configuration") + "\n\n")

	labels := fieldLabelsForMode(d.formMode)
	for i, label := range labels {
		labelStr := d.styles.ConfigKeyStyle.Render(label + ":")
		fieldView := d.formFields[i].View()

		if d.showPicker && d.picker != nil {
			if (label == "provider" && d.formMode == formAddMapping) ||
				(label == "behavior" && (d.formMode == formAddProvider || d.formMode == formEditProvider)) {
				fieldView = d.picker.View()
			}
		}

		fmt.Fprintf(&b, "  %s\n  %s\n", labelStr, fieldView)

		if errMsg, ok := d.fieldErrors[i]; ok {
			fmt.Fprintf(&b, "  %s\n", d.styles.StatusErrorStyle.Render(errMsg))
		}
	}

	if d.formError != "" {
		fmt.Fprintf(&b, "\n  %s\n", d.styles.StatusErrorStyle.Render(d.formError))
	}

	b.WriteString("\n")
	footer := d.styles.StatusClientErrStyle.Render("Enter=Save  Esc=Cancel  Tab=Next Field")
	b.WriteString("  " + footer)

	content := b.String()
	return d.styles.WindowStyle.Width(max(width-2, 0)).Render(content)
}

func renderDeleteConfirm(d *Dashboard, width int) string {
	msg := fmt.Sprintf("Delete %s '%s'? [y/N]", d.formKind, d.formEntryName)
	content := d.styles.StatusErrorStyle.Render(msg)
	return d.styles.WindowStyle.Width(max(width-2, 0)).Render(content)
}

func modalWidthFor(terminalWidth int) int {
	w := terminalWidth * 60 / 100
	return min(max(w, 40), 60)
}

func renderHelpModal(terminalWidth int, styles Styles) string {
	mw := modalWidthFor(terminalWidth)
	title := styles.ModalTitleStyle.Render(" Keyboard Shortcuts ")
	var rows []string
	for _, s := range helpShortcuts {
		key := styles.ShortcutKeyStyle.Width(14).Render(s.key)
		desc := styles.ShortcutDescStyle.Render(s.desc)
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, key, desc))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	footer := styles.ModalFooterStyle.Render("Press ? or Esc to close")
	sep := styles.SeparatorStyle.Render(strings.Repeat("─", mw-2))

	return styles.ModalStyle.
		Width(mw).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, sep, body, "", footer))
}

func overlayModal(_, modal string, width, height int, bgStyle lipgloss.Style) string {
	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		modal,
		lipgloss.WithWhitespaceStyle(bgStyle),
	)
}
