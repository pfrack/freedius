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
		"[3] Config",
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
		line := fmt.Sprintf(
			"%s  %s  %s  %s  %s",
			ts,
			statusStyled,
			truncate(model, 20),
			truncate(provider, 14),
			latency,
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
			"Provider", "Protocol", "Base URL", "Models",
		),
	)
	b.WriteString(header + "\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("─", max(width-4, 0))) + "\n")

	providers := collectProvidersFromConfig(cfg)
	for _, p := range providers {
		line := fmt.Sprintf(
			"%-14s %-10s %-30s %-6d",
			truncate(p.name, 14),
			truncate(p.protocol, 10),
			truncate(p.baseURL, 30),
			p.modelCount,
		)
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderConfigTab(cfg *config.Config, width int) string {
	var b strings.Builder
	b.WriteString(windowStyle.Render("Model Configuration") + "\n\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("─", max(width-4, 0))) + "\n")

	all := collectAllModels(cfg)
	for _, entry := range all {
		label := configKeyStyle.Render(entry.name)
		fmt.Fprintf(&b, "%s (%s):\n", label, entry.kind)
		fmt.Fprintf(&b, "  provider: %s\n", configValueStyle.Render(entry.model.Provider))
		fmt.Fprintf(&b, "  model:    %s\n", configValueStyle.Render(entry.model.Model))
		if entry.model.BaseURL != "" {
			fmt.Fprintf(&b, "  base_url: %s\n", configValueStyle.Render(entry.model.BaseURL))
		}
		if entry.model.Protocol != "" {
			fmt.Fprintf(&b, "  protocol: %s\n", configValueStyle.Render(entry.model.Protocol))
		}
		if entry.model.APIKeyEnv != "" {
			fmt.Fprintf(&b, "  api_key:  %s\n", configValueStyle.Render(entry.model.APIKeyEnv))
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
	name       string
	protocol   string
	baseURL    string
	modelCount int
}

func collectProvidersFromConfig(cfg *config.Config) []providerInfo {
	seen := map[string]*providerInfo{}
	incr := func(name, protocol, baseURL string) {
		if info, ok := seen[name]; ok {
			info.modelCount++
			return
		}
		seen[name] = &providerInfo{
			name:       name,
			protocol:   protocol,
			baseURL:    baseURL,
			modelCount: 1,
		}
	}
	for _, m := range cfg.Models {
		if m.OriginalProvider != "" {
			incr(m.OriginalProvider, m.Protocol, m.BaseURL)
		} else {
			incr(m.Provider, m.Protocol, m.BaseURL)
		}
	}
	for _, m := range cfg.Mappings {
		if m.OriginalProvider != "" {
			incr(m.OriginalProvider, m.Protocol, m.BaseURL)
		} else {
			incr(m.Provider, m.Protocol, m.BaseURL)
		}
	}
	result := make([]providerInfo, 0, len(seen))
	for _, info := range seen {
		result = append(result, *info)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].name < result[j].name
	})
	return result
}

type modelEntry struct {
	name  string
	kind  string
	model config.Model
}

func collectAllModels(cfg *config.Config) []modelEntry {
	var entries []modelEntry
	for name, m := range cfg.Models {
		entries = append(entries, modelEntry{name: name, kind: "model", model: m})
	}
	for name, m := range cfg.Mappings {
		entries = append(entries, modelEntry{name: name, kind: "mapping", model: m})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].kind != entries[j].kind {
			return entries[i].kind < entries[j].kind
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
