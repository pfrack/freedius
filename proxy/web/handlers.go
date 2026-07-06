// Package web provides the embedded web server for the freedius dashboard.
// It bundles static files (CSS, JS) via embed.FS and serves them through
// an embedded HTTP server.
package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
		renderPage(w, "index.html", indexData{
			pageData:    pageData{Active: "index"},
			Uptime:      uptime,
			TotalEvents: int64(h.Bus.EventCount()),
			TotalLogs:   h.LogSink.EventCount(),
			Port:        strconv.Itoa(h.Port),
			Host:        h.Host,
		}, logger)
	})
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, r *http.Request) {
		handleLogs(w, r, h.LogSink, logger)
	})
	mux.HandleFunc("GET /providers", func(w http.ResponseWriter, r *http.Request) {
		handleProviders(w, r, h.Cfg, logger)
	})
	mux.HandleFunc("GET /mappings", func(w http.ResponseWriter, r *http.Request) {
		handleMappings(w, r, h.Cfg, logger)
	})

	// Writeback: providers CRUD.
	mux.HandleFunc("POST /v1/providers", func(w http.ResponseWriter, r *http.Request) {
		handleCreateProvider(w, r, h.Cfg, h.CfgPath)
	})
	mux.HandleFunc("PUT /v1/providers/", func(w http.ResponseWriter, r *http.Request) {
		handleUpdateProvider(w, r, h.Cfg, h.CfgPath)
	})
	mux.HandleFunc("DELETE /v1/providers/", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteProvider(w, r, h.Cfg, h.CfgPath)
	})

	// Writeback: mappings CRUD.
	mux.HandleFunc("POST /v1/mappings", func(w http.ResponseWriter, r *http.Request) {
		handleCreateMapping(w, r, h.Cfg, h.CfgPath)
	})
	mux.HandleFunc("PUT /v1/mappings/", func(w http.ResponseWriter, r *http.Request) {
		handleUpdateMapping(w, r, h.Cfg, h.CfgPath)
	})
	mux.HandleFunc("DELETE /v1/mappings/", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteMapping(w, r, h.Cfg, h.CfgPath)
	})

	// Models endpoint: explicit refresh only.
	mux.HandleFunc("POST /v1/providers/{name}/models/refresh", func(w http.ResponseWriter, r *http.Request) {
		handleRefreshModels(w, r, h, logger)
	})

	return mux
}

// handleLogs renders the log page with server-rendered entries from the ring
// buffer. The ?min= query parameter filters by minimum log level.
func handleLogs(w http.ResponseWriter, r *http.Request, logSink *proxy.LogSink, logger *slog.Logger) {
	minRaw := r.URL.Query().Get("min")
	minLevel, err := parseMinLevel(minRaw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	entries, _, _ := logSink.SnapshotSince(0)

	// Collect the 200 most recent entries that pass the level filter,
	// iterating from newest to oldest to avoid building a large slice.
	const maxEntries = 200
	filtered := make([]logEntry, 0, maxEntries)
	for i := len(entries) - 1; i >= 0 && len(filtered) < maxEntries; i-- {
		e := entries[i]
		if minLevel != nil && e.Level < *minLevel {
			continue
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
		// HTMX request: render only the log fragment.
		tmpl, err := loadPageTemplate("logs.html")
		if err != nil {
			logger.Error("load template", "template", "logs.html", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		err = tmpl.ExecuteTemplate(w, "logs.html", logsData{
			Entries: filtered,
			Level:   levelSel,
		})
		if err != nil {
			logger.Error("execute template", "template", "logs.html", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
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
func handleProviders(w http.ResponseWriter, r *http.Request, cfg *config.Config, logger *slog.Logger) {
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
		renderProvidersTable(w, r, cfg)
	} else {
		// Direct visit: render full page.
		renderPage(w, "providers.html", providersData{
			pageData:  pageData{Active: "providers"},
			Providers: rows,
		}, logger, "providers-table.html")
	}
}

// handleMappings renders the mappings page with a read-only table.
func handleMappings(w http.ResponseWriter, r *http.Request, cfg *config.Config, logger *slog.Logger) {
	mappings := cfg.MappingsSnapshot()

	var rows []mappingRow
	for name, m := range mappings {
		var fallbacks []fallbackEntry
		for _, fb := range m.Fallback {
			fallbacks = append(fallbacks, fallbackEntry{
				ProviderName: fb.ProviderName,
				Model:        fb.ModelString,
			})
		}
		rows = append(rows, mappingRow{
			Name:         name,
			ProviderName: m.ProviderName,
			Model:        m.ModelString,
			Fallbacks:    fallbacks,
		})
	}

	// Provider list for the Add Mapping dropdown.
	providers := cfg.ProvidersSnapshot()
	mappingCounts := make(map[string]int)
	for _, m := range mappings {
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
		renderMappingsTable(w, r, cfg)
	} else {
		// Direct visit: render full page.
		renderPage(w, "mappings.html", mappingsData{
			pageData:  pageData{Active: "mappings"},
			Mappings:  rows,
			Providers: providerRows,
		}, logger, "mappings-table.html")
	}
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

// renderMappingsTable renders the `<table>` fragment for mappings.
func renderMappingsTable(w http.ResponseWriter, _ *http.Request, cfg *config.Config) {
	mappings := cfg.MappingsSnapshot()

	var rows []mappingRow
	for name, m := range mappings {
		var fallbacks []fallbackEntry
		for _, fb := range m.Fallback {
			fallbacks = append(fallbacks, fallbackEntry{
				ProviderName: fb.ProviderName,
				Model:        fb.ModelString,
			})
		}
		rows = append(rows, mappingRow{
			Name:         name,
			ProviderName: m.ProviderName,
			Model:        m.ModelString,
			Fallbacks:    fallbacks,
		})
	}

	// Provider list for the Add Mapping dropdown.
	providers := cfg.ProvidersSnapshot()
	mappingCounts := make(map[string]int)
	for _, m := range mappings {
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

func handleCreateProvider(w http.ResponseWriter, r *http.Request, cfg *config.Config, cfgPath string) {
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
		renderProvidersTable(w, r, cfg)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": name})
	}
}

func handleUpdateProvider(w http.ResponseWriter, r *http.Request, cfg *config.Config, cfgPath string) {
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
		renderProvidersTable(w, r, cfg)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": name})
	}
}

func handleDeleteProvider(w http.ResponseWriter, r *http.Request, cfg *config.Config, cfgPath string) {
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
		renderProvidersTable(w, r, cfg)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
	}
}

// --- Mapping writeback handlers ---

func handleCreateMapping(w http.ResponseWriter, r *http.Request, cfg *config.Config, cfgPath string) {
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
		renderMappingsTable(w, r, cfg)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": name})
	}
}

func handleUpdateMapping(w http.ResponseWriter, r *http.Request, cfg *config.Config, cfgPath string) {
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
		renderMappingsTable(w, r, cfg)
	} else {
		// Non-HTMX request: return JSON.
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": name})
	}
}

func handleDeleteMapping(w http.ResponseWriter, r *http.Request, cfg *config.Config, cfgPath string) {
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
		renderMappingsTable(w, r, cfg)
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
	if !mu.(*sync.Mutex).TryLock() {
		// Fetch already in progress — return cached data.
		models, _, _ := h.ModelsCache.Get(name)
		data := modelsData{Provider: name, Models: models}
		renderModelsFragment(w, data, logger)
		return
	}
	defer mu.(*sync.Mutex).Unlock()

	models, fetchErr := proxy.FetchModels(r.Context(), p)
	if fetchErr == nil {
		h.ModelsCache.Set(name, models, nil)
	}

	data := modelsData{
		Provider: name,
	}
	if models != nil {
		data.Models = models
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
