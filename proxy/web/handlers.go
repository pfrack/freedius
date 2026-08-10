// Package web provides the embedded web server for the freedius dashboard.
// It bundles static files (CSS, JS) via embed.FS and serves them through
// an embedded HTTP server.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

// modelFetchInflight prevents concurrent upstream fetches for the same provider.
var modelFetchInflight sync.Map

// testClient is a shared HTTP client for the provider "Test Connection" probe.
// It reuses one Transport (with idle-connection cleanup and proxy support)
// rather than allocating a fresh Transport per click, which would leak idle
// connections until the upstream dropped them. Redirects are treated as the
// final response (ErrUseLastResponse) because a reachability probe should not
// follow a provider URL to an unrelated host.
var testClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	},
}

// mappingStatus derives the routing-table/drawer status label and its CSS slug
// from live stats and the provider's API-key presence. A provider that declares
// no API-key env var (e.g. local Ollama, llama.cpp) is treated as OK — only a
// *declared-but-missing* key is flagged "Key Missing". This matches the
// attention-panel rule in attention.go (Rule 1) so the two never disagree.
func mappingStatus(ms proxy.MappingStats, envDeclared, envPresent bool) (string, string) {
	if envDeclared && !envPresent {
		return "Key Missing", "key-missing"
	}
	switch {
	case ms.RequestCount == 0:
		return "Unknown", "unknown"
	case ms.RecentErrorRate > 0.5:
		return "Degraded", "degraded"
	default:
		return "Healthy", "healthy"
	}
}

// csrfGuard blocks cross-origin mutating requests to the writeback API. The
// dashboard is served from 127.0.0.1 with an empty AuthToken by default, so a
// malicious page the operator visits could otherwise drive state-changing
// requests (including the outbound Test Connection call). Browsers send
// Sec-Fetch-Site for fetch/XHR/form submissions; HTMX requests from the
// same-origin dashboard carry "same-origin", so they pass untouched. GET
// routes (/v1/events, /v1/stats, /v1/config, /v1/logs) are read-only and
// exempt.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMutating(r.Method) && strings.HasPrefix(r.URL.Path, "/v1/") {
			if !requestIsSameOrigin(r) {
				http.Error(w, "cross-origin request blocked", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

func requestIsSameOrigin(r *http.Request) bool {
	// Preferred signal: browsers set Sec-Fetch-Site for fetch/XHR/form
	// submissions. Same-origin HTMX requests carry "same-origin".
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	// Fall back to an explicit Origin header when present.
	if origin := r.Header.Get("Origin"); origin != "" {
		return sameOriginHost(r.Host, origin)
	}
	// No browser fetch metadata and no Origin: treat as first-party
	// (e.g. curl, local tooling). The AuthToken boundary still applies if set.
	return true
}

// sameOriginHost reports whether origin (e.g. "http://localhost:8083") refers to
// the same host:port as host, filling in the default port for the scheme when a
// port is omitted.
func sameOriginHost(host, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	originHost := u.Hostname()
	originPort := u.Port()
	if originPort == "" {
		originPort = defaultPort(u.Scheme)
	}
	ph, err := url.Parse("http://" + host)
	if err != nil {
		return false
	}
	portHost := ph.Hostname()
	portPort := ph.Port()
	if portPort == "" {
		portPort = defaultPort("http")
	}
	return originHost == portHost && originPort == portPort
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// SetupMux builds the HTTP mux for the web server: page handlers, static
// assets, health check, eventstream routes, and writeback CRUD.
func SetupMux(h *eventstream.Handlers, logger *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()

	// Static assets.
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		serveStatic(w, r, logger)
	})

	// Health check.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Eventstream SSE/JSON routes.
	h.Register(mux)

	// Page handlers.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Go 1.22 ServeMux's `GET /` matches the root subtree, so any
		// unregistered GET path falls through here with 200 OK. Reclaim the
		// boundary: only the exact root serves the dashboard; everything
		// else gets the branded 404.
		if r.URL.Path != "/" {
			renderNotFound(w, logger)
			return
		}

		cfg := h.Cfg
		providers := cfg.ProvidersSnapshot()
		mappings := cfg.MappingsSnapshot()

		// Gather stats snapshots (nil-safe).
		var mappingStats map[string]proxy.MappingStats
		var providerStats map[string]proxy.ProviderStats
		if h.Stats != nil {
			mappingStats = h.Stats.MappingSnapshot()
			providerStats = h.Stats.ProviderSnapshot()
		}

		// Build health strip.
		uptime := time.Since(h.StartTime).Round(time.Second).String()
		endpoint := fmt.Sprintf("%s:%d", h.Host, h.Port)
		health := healthStrip{
			State:         "Healthy",
			Uptime:        uptime,
			Endpoint:      endpoint,
			LastRequest:   "No traffic",
			TotalRequests: int64(h.Bus.EventCount()),
		}
		// Compute last request time and error/fallback counts from stats.
		var lastReqTime time.Time
		for _, ms := range mappingStats {
			health.ErrorsLastHour += ms.ErrorCount
			health.FallbacksLast24h += ms.FallbackCount
			if ms.LastActivity.After(lastReqTime) {
				lastReqTime = ms.LastActivity
			}
		}
		if !lastReqTime.IsZero() {
			health.LastRequest = formatTimeAgo(lastReqTime)
		}
		// Determine overall state from provider stats.
		hasErrors := false
		for _, ps := range providerStats {
			if ps.RecentErrorRate > 0.5 && ps.RequestCount >= 3 {
				hasErrors = true
				break
			}
		}
		if hasErrors {
			health.State = "Degraded"
		}

		// Compute attention alerts.
		alerts := computeAlerts(cfg, mappingStats, providerStats, providers)

		// Build routing table rows.
		var rows []routingTableRow
		for name, m := range mappings {
			fallbackSummary := ""
			fallbackCount := len(m.Fallback)
			if fallbackCount > 0 {
				fb := m.Fallback[0]
				fallbackSummary = fb.ProviderName + " / " + fb.ModelString
			}
			ms := mappingStats[name]
			lastActivity := "No traffic"
			if !ms.LastActivity.IsZero() {
				lastActivity = formatTimeAgo(ms.LastActivity)
			}
			envDeclared := false
			envPresent := false
			if p, ok := providers[m.ProviderName]; ok {
				if p.DefaultAPIKeyEnv != "" {
					envDeclared = true
					envPresent = os.Getenv(p.DefaultAPIKeyEnv) != ""
				}
			}
			statusLabel, statusSlug := mappingStatus(ms, envDeclared, envPresent)
			rows = append(rows, routingTableRow{
				Name:            name,
				ProviderName:    m.ProviderName,
				Model:           m.ModelString,
				FallbackSummary: fallbackSummary,
				FallbackCount:   fallbackCount,
				StatusLabel:     statusLabel,
				StatusSlug:      statusSlug,
				RequestCount:    ms.RequestCount,
				ErrorCount:      ms.ErrorCount,
				FallbackEvents:  ms.FallbackCount,
				LastActivity:    lastActivity,
				EnvPresent:      envPresent,
			})
		}

		// Build provider health summary.
		mappingCounts := make(map[string]int)
		for _, m := range mappings {
			mappingCounts[m.ProviderName]++
		}
		phSummary := providerHealthSummary{
			Total: len(providers),
		}
		for name := range providers {
			ps := providerStats[name]
			status := deriveProviderStatus(ps)
			switch status {
			case "healthy":
				phSummary.Healthy++
			case "degraded":
				phSummary.Degraded++
			case "error":
				phSummary.Error++
			default:
				phSummary.Unknown++
			}
			lastChecked := "Never"
			if !ps.LastSuccess.IsZero() {
				lastChecked = formatTimeAgo(ps.LastSuccess)
			} else if !ps.LastError.IsZero() {
				lastChecked = formatTimeAgo(ps.LastError)
			}
			phSummary.Badges = append(phSummary.Badges, providerHealthBadge{
				Name:         name,
				Status:       status,
				LastChecked:  lastChecked,
				MappingCount: mappingCounts[name],
			})
		}

		// Build recent activity from EventBus ring (last 20), newest first.
		var recentActivity []activityRow
		events := h.Bus.Recent(20)
		for i := len(events) - 1; i >= 0; i-- {
			ev := events[i]
			if ev.Model == "" {
				continue
			}
			statusLabel := "OK"
			if ev.Status >= 400 {
				statusLabel = ev.ErrorType
				if statusLabel == "" {
					statusLabel = fmt.Sprintf("%d", ev.Status)
				}
			}
			recentActivity = append(recentActivity, activityRow{
				Timestamp:    ev.Timestamp.Format("15:04:05"),
				Mapping:      ev.Model,
				Route:        ev.MatchedProvider + " / " + ev.MatchedModel,
				FallbackUsed: false, // TODO: enrich from LastResponder
				Latency:      formatLatency(ev.Latency),
				Status:       ev.Status,
				StatusLabel:  statusLabel,
				LogsLink:     fmt.Sprintf("/logs?mapping=%s&provider=%s", ev.Model, ev.MatchedProvider),
			})
		}

		renderPage(w, "index.html", dashboardData{
			pageData:       pageData{Active: "index"},
			Health:         health,
			Alerts:         alerts,
			Rows:           rows,
			ProviderHealth: phSummary,
			RecentActivity: recentActivity,
			CurrentSeq:     h.Bus.CurrentSeq(),
		}, logger)
	})
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, r *http.Request) {
		handleLogs(w, r, h.LogSink, logger)
	})
	mux.HandleFunc("GET /providers", func(w http.ResponseWriter, r *http.Request) {
		handleProviders(w, r, h, logger)
	})
	mux.HandleFunc("GET /mappings", func(w http.ResponseWriter, r *http.Request) {
		handleMappings(w, r, h, logger)
	})

	// Writeback: providers CRUD.
	mux.HandleFunc("POST /v1/providers", func(w http.ResponseWriter, r *http.Request) {
		handleCreateProvider(w, r, h, h.CfgPath)
	})
	mux.HandleFunc("PUT /v1/providers/", func(w http.ResponseWriter, r *http.Request) {
		handleUpdateProvider(w, r, h, h.CfgPath)
	})
	mux.HandleFunc("DELETE /v1/providers/", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteProvider(w, r, h, h.CfgPath)
	})

	// Writeback: mappings CRUD.
	mux.HandleFunc("POST /v1/mappings", func(w http.ResponseWriter, r *http.Request) {
		handleCreateMapping(w, r, h, h.CfgPath)
	})
	mux.HandleFunc("PUT /v1/mappings/", func(w http.ResponseWriter, r *http.Request) {
		handleUpdateMapping(w, r, h, h.CfgPath)
	})
	mux.HandleFunc("DELETE /v1/mappings/", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteMapping(w, r, h, h.CfgPath)
	})
	mux.HandleFunc("GET /v1/mappings/last-responders", func(w http.ResponseWriter, _ *http.Request) {
		snap := h.LastResponder.Snapshot()
		if snap == nil {
			snap = map[string]int{}
		}
		writeJSON(w, http.StatusOK, snap)
	})

	// Drawer endpoint: HTMX-loaded fragment for the mapping details drawer.
	mux.HandleFunc("GET /v1/mappings/{name}/detail", func(w http.ResponseWriter, r *http.Request) {
		handleMappingDetail(w, r, h, logger)
	})

	// Drawer endpoint: HTMX-loaded fragment for the provider details drawer.
	mux.HandleFunc("GET /v1/providers/{name}/detail", func(w http.ResponseWriter, r *http.Request) {
		handleProviderDetail(w, r, h, logger)
	})

	// Models endpoint: explicit refresh only.
	mux.HandleFunc("POST /v1/providers/{name}/models/refresh", func(w http.ResponseWriter, r *http.Request) {
		handleRefreshModels(w, r, h, logger)
	})

	// Test connection: lightweight reachability check.
	mux.HandleFunc("POST /v1/providers/{name}/test", func(w http.ResponseWriter, r *http.Request) {
		handleTestConnection(w, r, h, logger)
	})

	return mux
}

// handleLogs renders the log page with server-rendered entries from the ring
// buffer. Filters: ?min=<level>, ?provider=<name>, ?mapping=<name>,
// ?outcome=<success|error>, ?fallback=<true|false> — all optional;
// multiple combine via AND. Provider/matching is a case-insensitive
// substring match against the rendered log line.
func handleLogs(w http.ResponseWriter, r *http.Request, logSink *proxy.LogSink, logger *slog.Logger) {
	q := r.URL.Query()
	minRaw := q.Get("min")
	minLevel, err := parseMinLevel(minRaw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	providerFilter := strings.ToLower(strings.TrimSpace(q.Get("provider")))
	mappingFilter := strings.ToLower(strings.TrimSpace(q.Get("mapping")))
	outcomeFilter := parseOutcomeFilter(q.Get("outcome"))
	fallbackFilter := parseFallbackFilter(q.Get("fallback"))

	entries, _, _ := logSink.SnapshotSince(0)

	// Collect the 200 most recent entries that pass every filter, iterating
	// from newest to oldest to avoid building a large slice.
	const maxEntries = 200
	filtered := make([]logEntry, 0, maxEntries)
	for i := len(entries) - 1; i >= 0 && len(filtered) < maxEntries; i-- {
		e := entries[i]
		if minLevel != nil && e.Level < *minLevel {
			continue
		}
		if providerFilter != "" || mappingFilter != "" {
			line := strings.ToLower(e.Line)
			if providerFilter != "" && !strings.Contains(line, providerFilter) {
				continue
			}
			if mappingFilter != "" && !strings.Contains(line, mappingFilter) {
				continue
			}
		}
		if outcomeFilter != "" {
			isError := e.Level >= slog.LevelError
			if outcomeFilter == "error" && !isError {
				continue
			}
			if outcomeFilter == "success" && isError {
				continue
			}
		}
		if fallbackFilter != "" {
			hasFallback := strings.Contains(strings.ToLower(e.Line), "fallback")
			if fallbackFilter == "true" && !hasFallback {
				continue
			}
			if fallbackFilter == "false" && hasFallback {
				continue
			}
		}
		filtered = append(filtered, logEntry{
			Level: eventstream.LevelLabel(e.Level),
			Line:  e.Line,
		})
	}
	// Reverse to chronological order.
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	// Selected-level label for the dropdown; "" when no filter (logs.html's
	// `{{if not .Level}}selected{{end}}` highlights "All").
	levelSel := ""
	if minLevel != nil {
		levelSel = eventstream.LevelLabel(*minLevel)
	}

	if r.Header.Get("HX-Request") == "true" {
		// HTMX request: render only the log entries fragment.
		renderLogEntries(w, filtered)
	} else {
		// Direct visit: render full page.
		renderPage(w, "logs.html", logsData{
			pageData: pageData{Active: "logs"},
			Entries:  filtered,
			Level:    levelSel,
			Provider: providerFilter,
			Mapping:  mappingFilter,
			Outcome:  outcomeFilter,
			Fallback: fallbackFilter,
		}, logger)
	}
}

// parseOutcomeFilter normalizes the ?outcome= query parameter. Returns ""
// for empty/invalid values (no filter applied). Valid values: "success",
// "error".
func parseOutcomeFilter(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success", "error":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return ""
	}
}

// parseFallbackFilter normalizes the ?fallback= query parameter. Returns ""
// for empty/invalid values (no filter applied). Valid values: "true", "false".
func parseFallbackFilter(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "false":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return ""
	}
}

// handleProviders renders the providers page with a read-only table.
// Filter: ?provider=<name> — optional, case-insensitive substring match on the
// provider name (mirrors handleLogs). The dashboard provider drawer's edit link
// uses it to open the page pre-filtered to a single provider.
func handleProviders(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger) {
	cfg := h.Cfg
	providerFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	providers := cfg.ProvidersSnapshot()
	mappings := cfg.MappingsSnapshot()

	// Count mappings per provider.
	counts := make(map[string]int)
	for _, m := range mappings {
		counts[m.ProviderName]++
	}

	// Gather provider stats (nil-safe).
	var providerStats map[string]proxy.ProviderStats
	if h.Stats != nil {
		providerStats = h.Stats.ProviderSnapshot()
	}

	var rows []providerRow
	for name, p := range providers {
		if providerFilter != "" && !strings.Contains(strings.ToLower(name), providerFilter) {
			continue
		}
		row := providerRow{
			Name:         name,
			Behavior:     p.Behavior,
			BaseURL:      p.DefaultBaseURL,
			APIKeyEnv:    p.DefaultAPIKeyEnv,
			Protocol:     p.Protocol,
			MappingCount: counts[name],
		}
		if ps, ok := providerStats[name]; ok && ps.RequestCount > 0 {
			row.Status = deriveProviderStatus(ps)
			row.RequestCount = ps.RequestCount
			if !ps.LastSuccess.IsZero() {
				row.LastSuccess = formatTimeAgo(ps.LastSuccess)
			}
			if !ps.LastError.IsZero() {
				row.LastError = formatTimeAgo(ps.LastError)
				row.LastErrorMessage = ps.LastErrorMessage
			}
		} else {
			row.Status = "unknown"
		}
		rows = append(rows, row)
	}

	// HTMX request: render only the table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderProvidersTable(w, r, h)
	} else {
		// Direct visit: render full page.
		renderPage(w, "providers.html", providersData{
			pageData:  pageData{Active: "providers"},
			Providers: rows,
		}, logger, "providers-table.html")
	}
}

// deriveProviderStatus computes a passive health label from ProviderStats.
// error rate > 50% → "degraded", last 3 consecutive errors → "error",
// no traffic → "unknown", otherwise "healthy".
// deriveProviderStatus is the single source of truth for a provider's health
// badge, used by both the dashboard health summary and the providers page so
// the two never disagree. A provider with no traffic is "unknown"; a high
// recent error rate (>=50%) is "degraded"; a most-recent failure
// (LastError after LastSuccess) is "error"; otherwise "healthy".
func deriveProviderStatus(ps proxy.ProviderStats) string {
	switch {
	case ps.RequestCount == 0:
		return "unknown"
	case ps.RecentErrorRate > 0.5:
		return "degraded"
	case ps.LastError.After(ps.LastSuccess):
		return "error"
	default:
		return "healthy"
	}
}

// mappingFilters is the set of active query-string filters for the
// mappings page. Empty fields mean "no filter applied". All filters
// combine with AND logic; per Phase 4 plan §4 (Mappings Page Table
// Refactor) this preserves the existing ?provider= behavior while
// adding ?search=, ?status=, and ?has_fallback=.
type mappingFilters struct {
	Search         string
	ProviderFilter string
	StatusFilter   string // "active", "inactive", or ""
	HasFallback    string // "true", "false", or ""
}

// parseMappingFilters pulls the active query-string filters from the
// request. Unknown or empty values become "" (no filter).
func parseMappingFilters(q url.Values) mappingFilters {
	return mappingFilters{
		Search:         strings.TrimSpace(q.Get("search")),
		ProviderFilter: strings.TrimSpace(q.Get("provider")),
		StatusFilter:   strings.TrimSpace(q.Get("status")),
		HasFallback:    strings.TrimSpace(q.Get("has_fallback")),
	}
}

// handleMappings renders the mappings page with a read-only table.
func handleMappings(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger) {
	cfg := h.Cfg
	providers := cfg.ProvidersSnapshot()
	filters := parseMappingFilters(r.URL.Query())

	rows := buildMappingRows(cfg, providers, h.LastResponder, filters)

	mappingCounts := make(map[string]int)
	for _, m := range cfg.MappingsSnapshot() {
		mappingCounts[m.ProviderName]++
	}
	var providerRows []providerRow
	for name, p := range providers {
		providerRows = append(providerRows, providerRow{
			Name:         name,
			Behavior:     p.Behavior,
			BaseURL:      p.DefaultBaseURL,
			APIKeyEnv:    p.DefaultAPIKeyEnv,
			Protocol:     p.Protocol,
			MappingCount: mappingCounts[name],
		})
	}

	// HTMX request: render only the table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderMappingsTable(w, r, h)
	} else {
		// Direct visit: render full page.
		renderPage(w, "mappings.html", mappingsData{
			pageData:          pageData{Active: "mappings"},
			Mappings:          rows,
			Providers:         providerRows,
			TotalMappings:     len(cfg.MappingsSnapshot()),
			Search:            filters.Search,
			ProviderFilter:    filters.ProviderFilter,
			StatusFilter:      filters.StatusFilter,
			HasFallbackFilter: filters.HasFallback,
		}, logger, "mappings-routing-table.html")
	}
}

// buildMappingRows builds the mapping rows for template rendering.
// It applies all active filters in mappingFilters (combined with AND).
// Returns an empty slice when the config has no mappings or every
// mapping is filtered out.
func buildMappingRows(
	cfg *config.Config,
	providers map[string]config.Provider,
	lastResponder *proxy.LastResponder,
	filters mappingFilters,
) []mappingRow {
	mappings := cfg.MappingsSnapshot()

	// Cache lowercased filters once outside the loop — ToLower allocates.
	searchLower := strings.ToLower(filters.Search)
	hasSearch := searchLower != ""
	providerLower := strings.ToLower(filters.ProviderFilter)
	hasProvider := providerLower != ""
	hasStatus := filters.StatusFilter == "active" || filters.StatusFilter == "inactive"
	hasFallback := filters.HasFallback == "true" || filters.HasFallback == "false"

	var rows []mappingRow
	for name, m := range mappings {
		// Apply provider filter (substring match on primary or any fallback).
		if hasProvider {
			matched := strings.Contains(strings.ToLower(m.ProviderName), providerLower)
			if !matched {
				for _, fb := range m.Fallback {
					if strings.Contains(strings.ToLower(fb.ProviderName), providerLower) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}

		// Apply search filter (case-insensitive substring match on mapping name).
		if hasSearch && !strings.Contains(strings.ToLower(name), searchLower) {
			continue
		}

		// Compute env presence + status for the filter and the badge.
		envPresent := false
		if p, ok := providers[m.ProviderName]; ok && p.DefaultAPIKeyEnv != "" {
			envPresent = os.Getenv(p.DefaultAPIKeyEnv) != ""
		}
		if hasStatus {
			switch filters.StatusFilter {
			case "active":
				if !envPresent {
					continue
				}
			case "inactive":
				if envPresent {
					continue
				}
			}
		}

		// Compute fallback count for the filter.
		fbCount := len(m.Fallback)
		if hasFallback {
			wantFallback := filters.HasFallback == "true"
			if wantFallback != (fbCount > 0) {
				continue
			}
		}

		var fallbacks []fallbackEntry
		for _, fb := range m.Fallback {
			fbProto := ""
			fbURL := ""
			if p, ok := providers[fb.ProviderName]; ok {
				fbProto = p.Protocol
				fbURL = p.DefaultBaseURL
			}
			fallbacks = append(fallbacks, fallbackEntry{
				ProviderName: fb.ProviderName,
				Model:        fb.ModelString,
				Protocol:     fbProto,
				BaseURL:      fbURL,
			})
		}
		proto := ""
		url := ""
		if p, ok := providers[m.ProviderName]; ok {
			proto = p.Protocol
			url = p.DefaultBaseURL
		}
		responder, hasResp := 0, false
		if lastResponder != nil {
			responder, hasResp = lastResponder.Lookup(name)
		}
		row := mappingRow{
			Name:         name,
			ProviderName: m.ProviderName,
			Model:        m.ModelString,
			Protocol:     proto,
			BaseURL:      url,
			Responder:    responder,
			HasResponder: hasResp,
			Fallbacks:    fallbacks,
			AddedAt:      m.AddedAt,
			EnvPresent:   envPresent,
			Protected:    name == "default",
		}
		rows = append(rows, row)
	}
	return rows
}

// parseMinLevel parses a ?min= query parameter into a slog.Level. Returns nil
// for the empty string (no filtering). Returns an error for non-empty unknown
// values per plan §2.9 ("?min=invalid returns 400 with JSON error").
func parseMinLevel(s string) (*slog.Level, error) {
	switch strings.ToLower(s) {
	case "":
		return nil, nil
	case "debug":
		l := slog.LevelDebug
		return &l, nil
	case "info":
		l := slog.LevelInfo
		return &l, nil
	case "warn":
		l := slog.LevelWarn
		return &l, nil
	case "error":
		l := slog.LevelError
		return &l, nil
	default:
		return nil, fmt.Errorf("min must be one of debug|info|warn|error, got %q", s)
	}
}

// renderProvidersTable renders the `<table>` fragment for providers.
// Re-enriches rows with live stats so the fragment matches the page render.
func renderProvidersTable(w http.ResponseWriter, _ *http.Request, h *eventstream.Handlers) {
	cfg := h.Cfg
	providers := cfg.ProvidersSnapshot()
	mappings := cfg.MappingsSnapshot()

	// Count mappings per provider.
	counts := make(map[string]int)
	for _, m := range mappings {
		counts[m.ProviderName]++
	}

	// Gather provider stats (nil-safe).
	var providerStats map[string]proxy.ProviderStats
	if h.Stats != nil {
		providerStats = h.Stats.ProviderSnapshot()
	}

	var rows []providerRow
	for name, p := range providers {
		row := providerRow{
			Name:         name,
			Behavior:     p.Behavior,
			BaseURL:      p.DefaultBaseURL,
			APIKeyEnv:    p.DefaultAPIKeyEnv,
			Protocol:     p.Protocol,
			MappingCount: counts[name],
		}
		if ps, ok := providerStats[name]; ok && ps.RequestCount > 0 {
			row.Status = deriveProviderStatus(ps)
			row.RequestCount = ps.RequestCount
			if !ps.LastSuccess.IsZero() {
				row.LastSuccess = formatTimeAgo(ps.LastSuccess)
			}
			if !ps.LastError.IsZero() {
				row.LastError = formatTimeAgo(ps.LastError)
				row.LastErrorMessage = ps.LastErrorMessage
			}
		} else {
			row.Status = "unknown"
		}
		rows = append(rows, row)
	}

	tmpl, err := loadFragmentTemplate("providers-table.html")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	// ExecuteTemplate has already written the 200 and part of the body, so an
	// error here cannot be turned into an HTTP error response — doing so
	// triggers "superfluous response.WriteHeader". Log only.
	if err := tmpl.ExecuteTemplate(w, "providers-table", providersData{
		Providers: rows,
	}); err != nil {
		slog.Error("execute providers table template", "err", err)
	}
}

// renderLogEntries renders the log entries fragment for HTMX requests.
func renderLogEntries(w http.ResponseWriter, entries []logEntry) {
	tmpl, err := loadFragmentTemplate("log-entries.html")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	// Response already committed by ExecuteTemplate — log only (see
	// renderProvidersTable).
	if err := tmpl.ExecuteTemplate(w, "log-entries", logsData{
		Entries: entries,
	}); err != nil {
		slog.Error("execute log entries template", "err", err)
	}
}

// renderMappingsTable renders the `<table>` fragment for mappings.
// Loads the new mappings-routing-table template (Phase 4) which renders
// a compact table + filter bar + delete-confirm dialog. The filter
// query params are parsed via parseMappingFilters so the HTMX fragment
// swap carries the same filter state as the page-level render.
func renderMappingsTable(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers) {
	cfg := h.Cfg
	providers := cfg.ProvidersSnapshot()
	filters := parseMappingFilters(r.URL.Query())

	rows := buildMappingRows(cfg, providers, h.LastResponder, filters)

	mappingCounts := make(map[string]int)
	for _, m := range cfg.MappingsSnapshot() {
		mappingCounts[m.ProviderName]++
	}
	var providerRows []providerRow
	for name, p := range providers {
		providerRows = append(providerRows, providerRow{
			Name:         name,
			Behavior:     p.Behavior,
			BaseURL:      p.DefaultBaseURL,
			APIKeyEnv:    p.DefaultAPIKeyEnv,
			Protocol:     p.Protocol,
			MappingCount: mappingCounts[name],
		})
	}

	tmpl, err := loadFragmentTemplate("mappings-routing-table.html")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	// Response already committed by ExecuteTemplate — log only (see
	// renderProvidersTable).
	if err := tmpl.ExecuteTemplate(w, "mappings-routing-table", mappingsData{
		Mappings:          rows,
		Providers:         providerRows,
		TotalMappings:     len(cfg.MappingsSnapshot()),
		Search:            filters.Search,
		ProviderFilter:    filters.ProviderFilter,
		StatusFilter:      filters.StatusFilter,
		HasFallbackFilter: filters.HasFallback,
	}); err != nil {
		slog.Error("execute mappings table template", "err", err)
	}
}

// handleMappingDetail renders the mapping details drawer fragment for a
// named mapping. HTMX-only endpoint — never returns a full page. Returns
// 404 JSON when the mapping is unknown so callers (the dashboard row
// handler) get a structured error rather than an empty HTML shell.
func handleMappingDetail(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_path", "missing mapping name")
		return
	}

	mappings := h.Cfg.MappingsSnapshot()
	m, ok := mappings[name]
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not_found", "mapping not found")
		return
	}

	providers := h.Cfg.ProvidersSnapshot()
	proto := ""
	baseURL := ""
	envDeclared := false
	envPresent := false
	if p, ok := providers[m.ProviderName]; ok {
		proto = p.Protocol
		baseURL = p.DefaultBaseURL
		if p.DefaultAPIKeyEnv != "" {
			envDeclared = true
			envPresent = os.Getenv(p.DefaultAPIKeyEnv) != ""
		}
	}

	var fallbacks []drawerFallback
	for _, fb := range m.Fallback {
		fallbacks = append(fallbacks, drawerFallback{
			Model:        fb.ModelString,
			ProviderName: fb.ProviderName,
		})
	}

	var ms proxy.MappingStats
	if h.Stats != nil {
		ms = h.Stats.MappingSnapshot()[name]
	}
	lastActivity := "No traffic"
	if !ms.LastActivity.IsZero() {
		lastActivity = formatTimeAgo(ms.LastActivity)
	}
	statusLabel, statusSlug := mappingStatus(ms, envDeclared, envPresent)

	data := drawerData{
		Name:           name,
		StatusLabel:    statusLabel,
		StatusSlug:     statusSlug,
		Model:          m.ModelString,
		ProviderName:   m.ProviderName,
		Protocol:       proto,
		BaseURL:        baseURL,
		Fallbacks:      fallbacks,
		RequestCount:   ms.RequestCount,
		ErrorCount:     ms.ErrorCount,
		FallbackEvents: ms.FallbackCount,
		LastActivity:   lastActivity,
		AddedAt:        m.AddedAt,
		EnvPresent:     envPresent,
	}

	tmpl, err := loadFragmentTemplate("mapping-drawer.html")
	if err != nil {
		logger.Error("load drawer template", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "mapping-drawer", data); err != nil {
		logger.Error("execute drawer template", "err", err)
	}
}

// handleProviderDetail renders the provider details drawer fragment for a
// named provider. HTMX-only endpoint — never returns a full page. Returns
// 404 JSON when the provider is unknown so callers (the dashboard badge
// handler) get a structured error rather than an empty HTML shell. Mirrors
// handleMappingDetail's not-found contract.
func handleProviderDetail(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_path", "missing provider name")
		return
	}

	providers := h.Cfg.ProvidersSnapshot()
	p, ok := providers[name]
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}

	// Gather provider stats (nil-safe, matching the sibling handlers).
	var ps proxy.ProviderStats
	if h.Stats != nil {
		ps = h.Stats.ProviderSnapshot()[name]
	}
	statusSlug := deriveProviderStatus(ps)
	statusLabel := statusSlugToLabel(statusSlug)

	envDeclared := p.DefaultAPIKeyEnv != ""
	envPresent := false
	if envDeclared {
		envPresent = os.Getenv(p.DefaultAPIKeyEnv) != ""
	}

	data := providerDrawerData{
		Name:        name,
		StatusLabel: statusLabel,
		StatusSlug:  statusSlug,
		Protocol:    p.Protocol,
		BaseURL:     p.DefaultBaseURL,
		EnvDeclared: envDeclared,
		EnvPresent:  envPresent,
		EditLink:    "/providers?provider=" + url.QueryEscape(name),
	}

	tmpl, err := loadFragmentTemplate("provider-drawer.html")
	if err != nil {
		logger.Error("load provider drawer template", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "provider-drawer", data); err != nil {
		logger.Error("execute provider drawer template", "err", err)
	}
}

// statusSlugToLabel maps a deriveProviderStatus slug to the human label used
// in drawer/health badges (Healthy/Degraded/Error/Unknown).
func statusSlugToLabel(slug string) string {
	switch slug {
	case "healthy":
		return "Healthy"
	case "degraded":
		return "Degraded"
	case "error":
		return "Error"
	default:
		return "Unknown"
	}
}

// --- Provider writeback handlers ---

func handleCreateProvider(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, cfgPath string) {
	cfg := h.Cfg
	name, p, err := decodeProviderForm(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_form", err.Error())
		return
	}
	if ve := validateProviderFields(name, p); ve != nil {
		writeValidationError(w, ve)
		return
	}

	cfg.Lock()
	old, hadOld := cfg.Providers[name]
	cfg.Providers[name] = p

	data, mErr := cfg.Marshal()
	if mErr != nil {
		if hadOld {
			cfg.Providers[name] = old
		} else {
			delete(cfg.Providers, name)
		}
		cfg.Unlock()
		writeJSONError(w, http.StatusInternalServerError, "marshal_failed", mErr.Error())
		return
	}
	if cfgPath != "" {
		if saveErr := cfg.SaveData(cfgPath, data); saveErr != nil {
			if hadOld {
				cfg.Providers[name] = old
			} else {
				delete(cfg.Providers, name)
			}
			cfg.Unlock()
			writeJSONError(w, http.StatusInternalServerError, "save_failed", saveErr.Error())
			return
		}
	}
	cfg.Unlock()
	// HTMX request: render the updated table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderProvidersTable(w, r, h)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": name})
	}
}

func handleUpdateProvider(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, cfgPath string) {
	cfg := h.Cfg
	name, err := pathName(r, "/v1/providers/")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_path", err.Error())
		return
	}
	_, p, err := decodeProviderForm(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_form", err.Error())
		return
	}
	if ve := validateProviderFields(name, p); ve != nil {
		writeValidationError(w, ve)
		return
	}

	cfg.Lock()
	old, existed := cfg.Providers[name]
	if !existed {
		cfg.Unlock()
		writeJSONError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	cfg.Providers[name] = p

	data, mErr := cfg.Marshal()
	if mErr != nil {
		cfg.Providers[name] = old
		cfg.Unlock()
		writeJSONError(w, http.StatusInternalServerError, "marshal_failed", mErr.Error())
		return
	}
	if cfgPath != "" {
		if saveErr := cfg.SaveData(cfgPath, data); saveErr != nil {
			cfg.Providers[name] = old
			cfg.Unlock()
			writeJSONError(w, http.StatusInternalServerError, "save_failed", saveErr.Error())
			return
		}
	}
	cfg.Unlock()
	// HTMX request: render the updated table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderProvidersTable(w, r, h)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": name})
	}
}

func handleDeleteProvider(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, cfgPath string) {
	cfg := h.Cfg
	name, err := pathName(r, "/v1/providers/")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_path", err.Error())
		return
	}

	cfg.Lock()
	_, existed := cfg.Providers[name]
	if !existed {
		cfg.Unlock()
		writeJSONError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	// Check if any mapping uses this provider.
	// Cannot call MappingsSnapshot() here — we already hold the write lock,
	// so RLock inside it would deadlock. Copy the map directly.
	for _, m := range cfg.Mappings {
		if m.ProviderName == name {
			cfg.Unlock()
			writeJSONError(w, http.StatusConflict, "provider_in_use", "mappings reference this provider")
			return
		}
		for _, fb := range m.Fallback {
			if fb.ProviderName == name {
				cfg.Unlock()
				writeJSONError(w, http.StatusConflict,
					"provider_in_use",
					"mappings reference this provider as fallback",
				)
				return
			}
		}
	}
	old := cfg.Providers[name]
	delete(cfg.Providers, name)

	data, mErr := cfg.Marshal()
	if mErr != nil {
		cfg.Providers[name] = old
		cfg.Unlock()
		writeJSONError(w, http.StatusInternalServerError, "marshal_failed", mErr.Error())
		return
	}
	if cfgPath != "" {
		if saveErr := cfg.SaveData(cfgPath, data); saveErr != nil {
			cfg.Providers[name] = old
			cfg.Unlock()
			writeJSONError(w, http.StatusInternalServerError, "save_failed", saveErr.Error())
			return
		}
	}
	cfg.Unlock()
	// HTMX request: render the updated table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderProvidersTable(w, r, h)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
	}
}

// --- Mapping writeback handlers ---

func handleCreateMapping(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, cfgPath string) {
	cfg := h.Cfg
	name, m, err := decodeMappingForm(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_form", err.Error())
		return
	}
	if ve := validateMappingFields(name, m, cfg); ve != nil {
		writeValidationError(w, ve)
		return
	}

	cfg.Lock()
	old, hadOld := cfg.Mappings[name]
	cfg.Mappings[name] = m

	data, mErr := cfg.Marshal()
	if mErr != nil {
		if hadOld {
			cfg.Mappings[name] = old
		} else {
			delete(cfg.Mappings, name)
		}
		cfg.Unlock()
		writeJSONError(w, http.StatusInternalServerError, "marshal_failed", mErr.Error())
		return
	}
	if cfgPath != "" {
		if saveErr := cfg.SaveData(cfgPath, data); saveErr != nil {
			if hadOld {
				cfg.Mappings[name] = old
			} else {
				delete(cfg.Mappings, name)
			}
			cfg.Unlock()
			writeJSONError(w, http.StatusInternalServerError, "save_failed", saveErr.Error())
			return
		}
	}
	cfg.Unlock()
	cfg.BuildMatchers()
	// HTMX request: render the updated table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderMappingsTable(w, r, h)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": name})
	}
}

func handleUpdateMapping(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, cfgPath string) {
	cfg := h.Cfg
	name, err := pathName(r, "/v1/mappings/")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_path", err.Error())
		return
	}
	_, m, err := decodeMappingForm(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_form", err.Error())
		return
	}
	if ve := validateMappingFields(name, m, cfg); ve != nil {
		writeValidationError(w, ve)
		return
	}

	cfg.Lock()
	old, existed := cfg.Mappings[name]
	if !existed {
		cfg.Unlock()
		writeJSONError(w, http.StatusNotFound, "not_found", "mapping not found")
		return
	}
	cfg.Mappings[name] = m

	data, mErr := cfg.Marshal()
	if mErr != nil {
		cfg.Mappings[name] = old
		cfg.Unlock()
		writeJSONError(w, http.StatusInternalServerError, "marshal_failed", mErr.Error())
		return
	}
	if cfgPath != "" {
		if saveErr := cfg.SaveData(cfgPath, data); saveErr != nil {
			cfg.Mappings[name] = old
			cfg.Unlock()
			writeJSONError(w, http.StatusInternalServerError, "save_failed", saveErr.Error())
			return
		}
	}
	cfg.Unlock()
	cfg.BuildMatchers()
	// HTMX request: render the updated table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderMappingsTable(w, r, h)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": name})
	}
}

func handleDeleteMapping(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, cfgPath string) {
	cfg := h.Cfg
	name, err := pathName(r, "/v1/mappings/")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_path", err.Error())
		return
	}

	// The default catch-all is always required, so refuse to delete it.
	if name == "default" {
		writeJSONError(w, http.StatusConflict, "protected_mapping",
			"the default mapping cannot be deleted")
		return
	}

	cfg.Lock()
	old, existed := cfg.Mappings[name]
	if !existed {
		cfg.Unlock()
		writeJSONError(w, http.StatusNotFound, "not_found", "mapping not found")
		return
	}
	delete(cfg.Mappings, name)

	data, mErr := cfg.Marshal()
	if mErr != nil {
		cfg.Mappings[name] = old
		cfg.Unlock()
		writeJSONError(w, http.StatusInternalServerError, "marshal_failed", mErr.Error())
		return
	}
	if cfgPath != "" {
		if saveErr := cfg.SaveData(cfgPath, data); saveErr != nil {
			cfg.Mappings[name] = old
			cfg.Unlock()
			writeJSONError(w, http.StatusInternalServerError, "save_failed", saveErr.Error())
			return
		}
	}
	cfg.Unlock()
	cfg.BuildMatchers()
	// HTMX request: render the updated table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderMappingsTable(w, r, h)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
	}
}

// --- Models handlers ---

func handleRefreshModels(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_path", "missing provider name")
		return
	}

	providers := h.Cfg.ProvidersSnapshot()
	p, ok := providers[name]
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}

	if p.DefaultBaseURL == "" {
		h.ModelsCache.Set(name, nil, fmt.Errorf("provider %q has no base URL configured", name))
		data := modelsData{
			Provider: name,
			Error:    fmt.Sprintf("Provider %q has no base URL configured", name),
		}
		renderModelsFragment(w, data, logger)
		return
	}

	// Deduplicate concurrent fetches for the same provider.
	mu, _ := modelFetchInflight.LoadOrStore(name, &sync.Mutex{})
	mtx, ok := mu.(*sync.Mutex)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !mtx.TryLock() {
		// Fetch already in progress — return cached data + in-progress hint.
		models, _, _ := h.ModelsCache.Get(name)
		data := modelsData{
			Provider:        name,
			Models:          models,
			FetchInProgress: true,
		}
		renderModelsFragment(w, data, logger)
		return
	}
	defer mtx.Unlock()

	models, fetchErr := proxy.FetchModels(r.Context(), p)
	if fetchErr == nil {
		h.ModelsCache.Set(name, models, nil)
	}

	data := modelsData{
		Provider: name,
	}
	if models != nil {
		// Cap the rendered model list server-side at 1000 entries. Anything
		// beyond is dropped here; the Truncated flag drives the user-facing
		// hint in models-fragment.html. Plan §F2.
		if len(models) > 1000 {
			data.Models = models[:1000]
			data.Truncated = true
		} else {
			data.Models = models
		}
	}
	if fetchErr != nil {
		data.Error = fetchErr.Error()
	}
	_, fetchedAt, _ := h.ModelsCache.Get(name)
	if !fetchedAt.IsZero() {
		data.FetchedAt = fmt.Sprintf("%s ago", formatDuration(time.Since(fetchedAt)))
	}

	renderModelsFragment(w, data, logger)
}

func renderModelsFragment(w http.ResponseWriter, data modelsData, logger *slog.Logger) {
	tmpl, err := loadFragmentTemplate("models-fragment.html")
	if err != nil {
		logger.Error("load fragment template", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		logger.Error("execute fragment template", "err", err)
	}
}

// handleTestConnection performs a lightweight reachability check against a
// provider's base URL and renders a simple success/failure fragment into
// the test-dialog-body. It does NOT fetch models — just checks if the
// endpoint responds (any HTTP status = reachable, connection error = unreachable).
func handleTestConnection(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_path", "missing provider name")
		return
	}

	providers := h.Cfg.ProvidersSnapshot()
	p, ok := providers[name]
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}

	result := testResultData{Provider: name}
	if p.DefaultBaseURL == "" {
		result.Reachable = false
		result.Message = "No base URL configured"
		renderTestResultFragment(w, result, logger)
		return
	}

	// Lightweight check: short timeout, any response (even 401/403) = reachable.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.DefaultBaseURL, nil)
	if err != nil {
		result.Reachable = false
		result.Message = fmt.Sprintf("Request error: %s", err)
		renderTestResultFragment(w, result, logger)
		return
	}

	start := time.Now()
	resp, err := testClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		result.Reachable = false
		result.Message = fmt.Sprintf("Connection failed: %s", err)
		renderTestResultFragment(w, result, logger)
		return
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	result.Reachable = true
	result.StatusCode = resp.StatusCode
	result.Latency = elapsed.Milliseconds()
	result.Message = fmt.Sprintf("Reachable (HTTP %d, %d ms)", resp.StatusCode, result.Latency)
	renderTestResultFragment(w, result, logger)
}

// testResultData is the data for the test-connection result fragment.
type testResultData struct {
	Provider   string
	Reachable  bool
	StatusCode int
	Latency    int64
	Message    string
}

func renderTestResultFragment(w http.ResponseWriter, data testResultData, logger *slog.Logger) {
	tmpl, err := loadFragmentTemplate("test-result.html")
	if err != nil {
		logger.Error("load test result template", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		logger.Error("execute test result template", "err", err)
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// formatTimeAgo renders a time.Time as a human-readable "Xm ago" string.
func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "Never"
	}
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Format("Jan 2 15:04")
}

// formatLatency renders a duration as a compact latency string.
func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// --- JSON response helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	buf, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"internal","message":"json marshal failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeValidationError(w http.ResponseWriter, ve *ValidationError) {
	w.Header().Set("Content-Type", "application/json")
	buf, err := json.Marshal(ve)
	if err != nil {
		http.Error(w, `{"error":"internal","message":"json marshal failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(buf)
}
