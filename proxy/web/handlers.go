// Package web provides the embedded web server for the freedius dashboard.
// It bundles static files (CSS, JS) via embed.FS and serves them through
// an embedded HTTP server.
package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

// modelFetchInflight prevents concurrent upstream fetches for the same provider.
var modelFetchInflight sync.Map

// SetupMux builds the HTTP mux for the web server: page handlers, static
// assets, health check, eventstream routes, and writeback CRUD.
func SetupMux(h *eventstream.Handlers, logger *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()

	// Static assets.
	mux.HandleFunc("GET /static/", serveStatic)

	// Health check.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Eventstream SSE/JSON routes.
	h.Register(mux)

	// Page handlers.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		uptime := time.Since(h.StartTime).Round(time.Second).String()
		cfg := h.Cfg
		providers := cfg.ProvidersSnapshot()

		// Build mapping rows (no filter for dashboard).
		mappings := buildMappingRows(cfg, providers, h.LastResponder, "")

		// Build provider rows with mapping counts.
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

		renderPage(w, "index.html", indexData{
			pageData:    pageData{Active: "index"},
			Uptime:      uptime,
			TotalEvents: int64(h.Bus.EventCount()),
			TotalLogs:   h.LogSink.EventCount(),
			Port:        strconv.Itoa(h.Port),
			Host:        h.Host,
			Mappings:    mappings,
			Providers:   providerRows,
		}, logger, "mappings-table.html")
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

	// Models endpoint: explicit refresh only.
	mux.HandleFunc("POST /v1/providers/{name}/models/refresh", func(w http.ResponseWriter, r *http.Request) {
		handleRefreshModels(w, r, h, logger)
	})

	return mux
}

// handleLogs renders the log page with server-rendered entries from the ring
// buffer. Filters: ?min=<level>, ?provider=<name>, ?mapping=<name> — all
// optional; multiple combine via AND. Provider/matching is a case-insensitive
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
		}, logger)
	}
}

// handleProviders renders the providers page with a read-only table.
func handleProviders(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger) {
	cfg := h.Cfg
	providers := cfg.ProvidersSnapshot()
	mappings := cfg.MappingsSnapshot()

	// Count mappings per provider.
	counts := make(map[string]int)
	for _, m := range mappings {
		counts[m.ProviderName]++
	}

	var rows []providerRow
	for name, p := range providers {
		rows = append(rows, providerRow{
			Name:         name,
			Behavior:     p.Behavior,
			BaseURL:      p.DefaultBaseURL,
			APIKeyEnv:    p.DefaultAPIKeyEnv,
			Protocol:     p.Protocol,
			MappingCount: counts[name],
		})
	}

	// HTMX request: render only the table fragment.
	if r.Header.Get("HX-Request") == "true" {
		renderProvidersTable(w, r, h.Cfg)
	} else {
		// Direct visit: render full page.
		renderPage(w, "providers.html", providersData{
			pageData:  pageData{Active: "providers"},
			Providers: rows,
		}, logger, "providers-table.html")
	}
}

// handleMappings renders the mappings page with a read-only table.
func handleMappings(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers, logger *slog.Logger) {
	cfg := h.Cfg
	providers := cfg.ProvidersSnapshot()
	providerFilter := r.URL.Query().Get("provider")

	rows := buildMappingRows(cfg, providers, h.LastResponder, providerFilter)

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
			pageData:  pageData{Active: "mappings"},
			Mappings:  rows,
			Providers: providerRows,
		}, logger, "mappings-table.html")
	}
}

// buildMappingRows builds the mapping rows for template rendering.
// It filters mappings by provider name (case-insensitive substring match)
// when providerFilter is non-empty.
func buildMappingRows(
	cfg *config.Config,
	providers map[string]config.Provider,
	lastResponder *proxy.LastResponder,
	providerFilter string,
) []mappingRow {
	mappings := cfg.MappingsSnapshot()

	// Cache lowercased filter once outside the loop — ToLower allocates.
	filterLower := strings.ToLower(providerFilter)
	hasFilter := filterLower != ""

	var rows []mappingRow
	for name, m := range mappings {
		// Apply provider filter if set.
		if hasFilter {
			// Check primary provider.
			if !strings.Contains(strings.ToLower(m.ProviderName), filterLower) {
				// Check fallback providers.
				matched := false
				for _, fb := range m.Fallback {
					if strings.Contains(strings.ToLower(fb.ProviderName), filterLower) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
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
		envPresent := false
		if p, ok := providers[m.ProviderName]; ok {
			proto = p.Protocol
			url = p.DefaultBaseURL
			if p.DefaultAPIKeyEnv != "" {
				envPresent = os.Getenv(p.DefaultAPIKeyEnv) != ""
			}
		}
		family, _ := proxy.ExtractFamily(name)
		if family == "default" {
			family = ""
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
			Family:       family,
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
func renderProvidersTable(w http.ResponseWriter, _ *http.Request, cfg *config.Config) {
	providers := cfg.ProvidersSnapshot()
	mappings := cfg.MappingsSnapshot()

	// Count mappings per provider.
	counts := make(map[string]int)
	for _, m := range mappings {
		counts[m.ProviderName]++
	}

	var rows []providerRow
	for name, p := range providers {
		rows = append(rows, providerRow{
			Name:         name,
			Behavior:     p.Behavior,
			BaseURL:      p.DefaultBaseURL,
			APIKeyEnv:    p.DefaultAPIKeyEnv,
			Protocol:     p.Protocol,
			MappingCount: counts[name],
		})
	}

	tmpl, err := loadFragmentTemplate("providers-table.html")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	err = tmpl.ExecuteTemplate(w, "providers-table", providersData{
		Providers: rows,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
	}
}

// renderLogEntries renders the log entries fragment for HTMX requests.
func renderLogEntries(w http.ResponseWriter, entries []logEntry) {
	tmpl, err := loadFragmentTemplate("log-entries.html")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	err = tmpl.ExecuteTemplate(w, "log-entries", logsData{
		Entries: entries,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
	}
}

// renderMappingsTable renders the `<table>` fragment for mappings.
func renderMappingsTable(w http.ResponseWriter, r *http.Request, h *eventstream.Handlers) {
	cfg := h.Cfg
	providers := cfg.ProvidersSnapshot()
	providerFilter := r.URL.Query().Get("provider")

	rows := buildMappingRows(cfg, providers, h.LastResponder, providerFilter)

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

	tmpl, err := loadFragmentTemplate("mappings-table.html")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
		return
	}
	err = tmpl.ExecuteTemplate(w, "mappings-table", mappingsData{
		Mappings:  rows,
		Providers: providerRows,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "template_failed", err.Error())
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
		renderProvidersTable(w, r, h.Cfg)
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
		renderProvidersTable(w, r, h.Cfg)
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
		renderProvidersTable(w, r, h.Cfg)
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
