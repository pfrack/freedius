package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

func renderTabs(active int, width int) string {
	tabs := []string{
		"[1] Log",
		"[2] Providers",
		"[3] Config (j/k=scroll Enter=edit a=+map p=+prov d=del)",
	}
	styled := make([]string, len(tabs))
	for i, t := range tabs {
		if i == active {
			styled[i] = activeTabStyle.Render(t)
		} else {
			styled[i] = inactiveTabStyle.Render(t)
		}
	}
	joined := lipgloss.JoinHorizontal(lipgloss.Top, styled...)
	return tabBarStyle.Width(max(width-2, 0)).Render(joined)
}

func renderLogTab(events []proxy.RequestEvent, _ int, height, scroll int) string {
	if len(events) == 0 {
		return windowStyle.Render("No requests yet...")
	}

	available := height - 4
	if available < 0 {
		available = 0
	}
	start := 0
	if len(events) > available {
		start = len(events) - available - scroll
		if start < 0 {
			start = 0
		}
	}
	end := start + available
	if end > len(events) {
		end = len(events)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		e := events[i]
		ts := e.Timestamp.Format("15:04:05")
		statusStr := fmt.Sprintf("%d", e.Status)
		var statusStyled string
		switch {
		case e.Status >= 500:
			statusStyled = statusErrorStyle.Render(statusStr)
		case e.Status >= 400:
			statusStyled = statusClientErrStyle.Render(statusStr)
		default:
			statusStyled = statusOKStyle.Render(statusStr)
		}
		duration := e.Latency.Milliseconds()
		errSuffix := ""
		if e.Status >= 400 && e.ErrorMessage != "" {
			errSuffix = " error=" + strconv.Quote(e.ErrorMessage)
		}
		line := fmt.Sprintf(
			"time=%s request_id=%s method=%s path=%s status=%s duration_ms=%d matched_provider=%s matched_model=%s%s",
			ts,
			e.RequestID,
			e.Method,
			e.Path,
			statusStyled,
			duration,
			e.MatchedProvider,
			e.MatchedModel,
			errSuffix,
		)
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderProvidersTab(cfg *config.Config, width, height, scroll int) string {
	var b strings.Builder
	b.WriteString(windowStyle.Render("Provider Configuration") + "\n\n")

	header := providerTableHeaderStyle.Render(
		fmt.Sprintf(
			"%-14s %-10s %-30s %-6s",
			"Provider", "Behavior", "Base URL", "Mappings",
		),
	)
	b.WriteString(header + "\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("─", max(width-4, 0))) + "\n")

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

func renderConfigTab(cfg *config.Config, cursor int, width, height int) string {
	var b strings.Builder
	b.WriteString(windowStyle.Render("Configuration") + "\n\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("─", max(width-4, 0))) + "\n")

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
		nameStyle := configKeyStyle
		if i == cursor {
			nameStyle = activeTabStyle
		}
		label := nameStyle.Render(entry.name)
		fmt.Fprintf(&b, "%s (%s):\n", label, entry.kind)
		if entry.kind == "provider" {
			provider := entry.provider
			fmt.Fprintf(&b, "  behavior: %s\n", configValueStyle.Render(provider.Behavior))
			if provider.DefaultBaseURL != "" {
				fmt.Fprintf(
					&b,
					"  base_url: %s\n",
					configValueStyle.Render(provider.DefaultBaseURL),
				)
			}
			if provider.DefaultAPIKeyEnv != "" {
				fmt.Fprintf(
					&b,
					"  api_key:  %s\n",
					configValueStyle.Render(provider.DefaultAPIKeyEnv),
				)
			}
			if provider.AnthropicVersion != "" {
				fmt.Fprintf(
					&b,
					"  api_ver:  %s\n",
					configValueStyle.Render(provider.AnthropicVersion),
				)
			}
		} else {
			mapping := entry.mapping
			fmt.Fprintf(&b, "  provider_name: %s\n", configValueStyle.Render(mapping.ProviderName))
			fmt.Fprintf(&b, "  model_string:  %s\n", configValueStyle.Render(mapping.ModelString))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderStatsBar(stats statsData, width int) string {
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
	return statsBarStyle.Render(line)
}

type providerInfo struct {
	name         string
	behavior     string
	baseURL      string
	mappingCount int
}

func collectProvidersFromConfig(cfg *config.Config) []providerInfo {
	result := make([]providerInfo, 0, len(cfg.Providers))
	for name, p := range cfg.Providers {
		mappingCount := 0
		for _, m := range cfg.Mappings {
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
	var entries []configEntry
	for name, p := range cfg.Providers {
		entries = append(entries, configEntry{name: name, kind: "provider", provider: p})
	}
	for name, m := range cfg.Mappings {
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
	b.WriteString(windowStyle.Render("Edit Configuration") + "\n\n")

	labels := fieldLabelsForMode(d.formMode)
	for i, label := range labels {
		labelStr := configKeyStyle.Render(label + ":")
		fieldView := d.formFields[i].View()

		if d.showPicker && d.picker != nil {
			if (label == "provider" && d.formMode == formAddMapping) ||
				(label == "behavior" && (d.formMode == formAddProvider || d.formMode == formEditProvider)) {
				fieldView = d.picker.View()
			}
		}

		fmt.Fprintf(&b, "  %s\n  %s\n", labelStr, fieldView)

		if errMsg, ok := d.fieldErrors[i]; ok {
			fmt.Fprintf(&b, "  %s\n", statusErrorStyle.Render(errMsg))
		}
	}

	if d.formError != "" {
		fmt.Fprintf(&b, "\n  %s\n", statusErrorStyle.Render(d.formError))
	}

	b.WriteString("\n")
	footer := statusClientErrStyle.Render("Enter=Save  Esc=Cancel  Tab=Next Field  Ctrl+D=Delete")
	b.WriteString("  " + footer)

	content := b.String()
	return windowStyle.Width(max(width-2, 0)).Render(content)
}

func renderDeleteConfirm(d *Dashboard, width int) string {
	msg := fmt.Sprintf("Delete %s '%s'? [y/N]", d.formKind, d.formEntryName)
	content := statusErrorStyle.Render(msg)
	return windowStyle.Width(max(width-2, 0)).Render(content)
}
