package tui

import (
	"fmt"
	"log/slog"
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
		"[2] Providers (Enter=edit p=+prov d=del)",
		"[3] Mappings (j/k=scroll Enter=edit a=+map d=del)",
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

func renderLogTab(entries []proxy.LogEntry, _ int, height, scroll int, filter LogFilter, styles Styles) string {
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
		var styled string
		switch {
		case e.Level >= slog.LevelError:
			styled = styles.StatusErrorStyle.Render(e.Line)
		case e.Level >= slog.LevelWarn:
			styled = styles.StatusClientErrStyle.Render(e.Line)
		case e.Level >= slog.LevelInfo:
			styled = styles.LogInfoStyle.Render(e.Line)
		default: // Debug
			styled = styles.LogDebugStyle.Render(e.Line)
		}
		b.WriteString(styled + "\n")
	}
	return b.String()
}

func renderProvidersTab(cfg *config.Config, cursor, width, height int, styles Styles) string {
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

	providers := collectProvidersFromConfig(cfg)
	available := height - 4
	if available < 0 {
		available = 0
	}

	visible := available
	if visible > len(providers) {
		visible = len(providers)
	}
	half := visible / 2
	start := cursor - half
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > len(providers) {
		end = len(providers)
		start = end - visible
		if start < 0 {
			start = 0
		}
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
		if i == cursor {
			line = styles.ActiveTabStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(providers) == 0 {
		b.WriteString("(no providers configured)\n")
	}
	return b.String()
}

func configVisibleWindow(entries []configEntry, cursor, available, entryLines int) (start, end int) {
	if available < 0 {
		available = 0
	}
	visibleEntries := available / entryLines
	if visibleEntries < 1 {
		visibleEntries = 1
	}
	if visibleEntries > len(entries) {
		visibleEntries = len(entries)
	}
	half := visibleEntries / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + visibleEntries
	if end > len(entries) {
		end = len(entries)
		start = end - visibleEntries
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func renderMappingsTab(cfg *config.Config, cursor int, width, height int, styles Styles) string {
	var b strings.Builder
	b.WriteString(styles.WindowStyle.Render("Mappings") + "\n\n")
	b.WriteString(styles.SeparatorStyle.Render(strings.Repeat("─", max(width-4, 0))) + "\n")

	all := collectMappingEntries(cfg)
	start, end := configVisibleWindow(all, cursor, height-3, 4)

	for i := start; i < end; i++ {
		entry := all[i]
		nameStyle := styles.ConfigKeyStyle
		if i == cursor {
			nameStyle = styles.ActiveTabStyle
		}
		label := nameStyle.Render(entry.name)
		fmt.Fprintf(&b, "%s (mapping):\n", label)
		mapping := entry.mapping
		fmt.Fprintf(&b, "  provider_name: %s\n", styles.ConfigValueStyle.Render(mapping.ProviderName))
		fmt.Fprintf(&b, "  model_string:  %s\n", styles.ConfigValueStyle.Render(mapping.ModelString))
		b.WriteString("\n")
	}
	if len(all) == 0 {
		b.WriteString(styles.ConfigValueStyle.Render("(no mappings configured)") + "\n")
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
	left := fmt.Sprintf(
		" uptime: %s │ requests: %d │ errors: %d │ error rate: %s ",
		uptime, total, errors, errRate,
	)
	if stats.message != "" {
		left = left + "│ " + stats.message + " "
	}
	right := "? for help "

	gap := width - len(left) - len(right)
	if gap < 1 {
		gap = 1
	}
	left += strings.Repeat(" ", gap)
	return styles.StatsBarPartStyle.Width(width).Render(left + right)
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

// configEntry is one row in the mappings tab.
type configEntry struct {
	name    string
	kind    string // "mapping"
	mapping config.Mapping
}

func collectMappingEntries(cfg *config.Config) []configEntry {
	mappings := cfg.MappingsSnapshot()
	var entries []configEntry
	for name, m := range mappings {
		entries = append(entries, configEntry{name: name, kind: "mapping", mapping: m})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}

// findEntryIndex returns the index of the entry with the given name and kind,
// or -1 if not found. Use this in tests instead of hardcoding cursor positions
// so the test survives changes to the sort order in collectAllEntries.
func findEntryIndex(cfg *config.Config, name, kind string) int {
	for i, e := range collectMappingEntries(cfg) {
		if e.name == name && e.kind == kind {
			return i
		}
	}
	return -1
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
			"protocol",
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

func renderProviderEditModal(terminalWidth int, d *Dashboard) string {
	mw := modalWidthFor(terminalWidth)
	var title string
	if d.formMode == formAddProvider {
		title = d.styles.ModalTitleStyle.Render(" Add New Provider ")
	} else {
		title = d.styles.ModalTitleStyle.Render(" Edit Provider: " + d.formEntryName + " ")
	}

	labels := fieldLabelsForMode(d.formMode)
	var rows []string
	for i, label := range labels {
		labelStr := d.styles.ConfigKeyStyle.Render(label + ":")
		fieldView := d.formFields[i].View()

		if d.showPicker && d.picker != nil {
			if (label == "behavior" && (d.formMode == formAddProvider || d.formMode == formEditProvider)) ||
				(label == "protocol" && (d.formMode == formAddProvider || d.formMode == formEditProvider)) {
				fieldView = d.picker.View()
			}
		}

		row := fmt.Sprintf("  %s\n  %s", labelStr, fieldView)
		if errMsg, ok := d.fieldErrors[i]; ok {
			row += fmt.Sprintf("\n  %s", d.styles.StatusErrorStyle.Render(errMsg))
		}
		rows = append(rows, row)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	footer := d.styles.ModalFooterStyle.Render("Enter=Save  Esc=Cancel  Tab=Next Field")
	sep := d.styles.SeparatorStyle.Render(strings.Repeat("─", mw-2))

	return d.styles.ModalStyle.
		Width(mw).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, sep, body, "", footer))
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
