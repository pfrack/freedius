package web

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

// computeAlerts evaluates attention rules and returns sorted alerts
// (errors first, then warnings). Pure function — no side effects.
func computeAlerts(
	cfg *config.Config,
	mappingStats map[string]proxy.MappingStats,
	providerStats map[string]proxy.ProviderStats,
	providers map[string]config.Provider,
) []attentionAlert {
	var alerts []attentionAlert

	mappings := cfg.MappingsSnapshot()

	// Rule 1: Missing API key env var for providers used by mappings.
	for mappingName, m := range mappings {
		p, ok := providers[m.ProviderName]
		if !ok {
			continue
		}
		if p.DefaultAPIKeyEnv != "" && os.Getenv(p.DefaultAPIKeyEnv) == "" {
			alerts = append(alerts, attentionAlert{
				Severity: "error",
				Message: fmt.Sprintf(
					"Mapping %q: provider %q missing API key (%s)",
					mappingName,
					m.ProviderName,
					p.DefaultAPIKeyEnv,
				),
				Link: "/providers",
				Icon: "key",
			})
		}
	}

	// Rule 2: Provider error rate > 50% in recent window.
	for name, ps := range providerStats {
		if ps.RecentErrorRate > 0.5 && ps.RequestCount >= 3 {
			alerts = append(alerts, attentionAlert{
				Severity: "warning",
				Message: fmt.Sprintf(
					"Provider %q: %.0f%% error rate in recent requests",
					name,
					ps.RecentErrorRate*100,
				),
				Link: fmt.Sprintf("/logs?provider=%s&min=error", name),
				Icon: "alert-triangle",
			})
		}
	}

	// Rule 3: Provider no success in last 5 minutes (but has traffic).
	fiveMinAgo := time.Now().Add(-5 * time.Minute)
	for name, ps := range providerStats {
		if ps.RequestCount > 0 && !ps.LastSuccess.IsZero() && ps.LastSuccess.Before(fiveMinAgo) &&
			ps.LastError.After(ps.LastSuccess) {
			alerts = append(alerts, attentionAlert{
				Severity: "warning",
				Message:  fmt.Sprintf("Provider %q: no successful request in last 5 minutes", name),
				Link:     fmt.Sprintf("/logs?provider=%s&min=error", name),
				Icon:     "clock",
			})
		}
	}

	// Rule 4: Mapping references a provider that doesn't exist in config.
	for mappingName, m := range mappings {
		if _, ok := providers[m.ProviderName]; !ok {
			alerts = append(alerts, attentionAlert{
				Severity: "error",
				Message: fmt.Sprintf(
					"Mapping %q references missing provider %q",
					mappingName,
					m.ProviderName,
				),
				Link: "/mappings",
				Icon: "x-circle",
			})
		}
	}

	// Rule 5: Mapping with high fallback usage.
	for name, ms := range mappingStats {
		if ms.FallbackCount > 0 && ms.RequestCount >= 5 {
			fallbackRate := float64(ms.FallbackCount) / float64(ms.RequestCount)
			if fallbackRate > 0.3 {
				alerts = append(alerts, attentionAlert{
					Severity: "warning",
					Message: fmt.Sprintf(
						"Mapping %q: %.0f%% requests using fallback",
						name,
						fallbackRate*100,
					),
					Link: fmt.Sprintf("/logs?mapping=%s", name),
					Icon: "git-branch",
				})
			}
		}
	}

	// Sort: errors first, then warnings, alphabetically within each.
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Severity != alerts[j].Severity {
			return alerts[i].Severity == "error" // errors before warnings
		}
		return alerts[i].Message < alerts[j].Message
	})

	return alerts
}
