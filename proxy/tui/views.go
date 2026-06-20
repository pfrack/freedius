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

func renderTabs(active int, width int) string {
	tabs := []string{
		"[1] Requests",
		"[2] Providers",
		"[3] Config (e=edit a=+map p=+prov d=del)",
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

func renderRequestsTab(events []proxy.RequestEvent, _ int, height int) string {
	if len(events) == 0 {
		return windowStyle.Render("No requests yet. Send a request to see it appear here.")
	}

	var b strings.Builder
	b.WriteString(windowStyle.Render("Request Log") + "\n\n")

	available := height - 4
	if available < 0 {
		available = 0
	}
	start := 0
	if len(events) > available {
		start = len(events) - available
	}

	for i := start; i < len(events); i++ {
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
		model := e.Model
		if model == "" {
			model = e.MatchedModel
		}
		provider := e.Provider
		if provider == "" {
			provider = "-"
		}
		latency := roundLatency(e.Latency)
		errMsg := ""
		if e.Status >= 400 && e.ErrorMessage != "" {
			errMsg = " " + errorMessageStyle.Render(truncate(e.ErrorMessage, 80))
		}
		line := fmt.Sprintf(
			"%s  %s  %s  %s  %s%s",
			ts,
			statusStyled,
			truncate(model, 20),
			truncate(provider, 14),
			latency,
			errMsg,
		)
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderProvidersTab(cfg *config.Config, width int) string {
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

	providers := collectProvidersFromConfig(cfg)
	for _, p := range providers {
		line := fmt.Sprintf(
			"%-14s %-10s %-30s %-6d",
			truncate(p.name, 14),
			truncate(p.behavior, 10),
			truncate(p.baseURL, 30),
			p.mappingCount,
		)
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderConfigTab(cfg *config.Config, cursor, width int) string {
	var b strings.Builder
	b.WriteString(windowStyle.Render("Configuration") + "\n\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("─", max(width-4, 0))) + "\n")

	all := collectAllEntries(cfg)
	for i, entry := range all {
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

func roundLatency(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Millisecond).String()
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
