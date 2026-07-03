// Package web provides the embedded web server for the freedius dashboard.
// It bundles static files (CSS, JS) via embed.FS and serves them through
// an embedded HTTP server.
package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
)

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
		renderPage(w, "index.html", indexData{pageData: pageData{Active: "index"}}, logger)
	})
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, r *http.Request) {
		handleLogs(w, r, h.LogSink, logger)
	})
	mux.HandleFunc("GET /providers", func(w http.ResponseWriter, _ *http.Request) {
		handleProviders(w, h.Cfg, logger)
	})
	mux.HandleFunc("GET /mappings", func(w http.ResponseWriter, _ *http.Request) {
		handleMappings(w, h.Cfg, logger)
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

	return mux
}

// handleLogs renders the log page with server-rendered entries from the ring
// buffer. The ?min= query parameter filters by minimum log level.
func handleLogs(w http.ResponseWriter, r *http.Request, logSink *proxy.LogSink, logger *slog.Logger) {
	minLevel := parseMinLevel(r.URL.Query().Get("min"))
	entries, _, _ := logSink.SnapshotSince(0)

	var filtered []logEntry
	for _, e := range entries {
		if minLevel != nil && e.Level < *minLevel {
			continue
		}
		filtered = append(filtered, logEntry{
			Level: levelLabel(e.Level),
			Line:  e.Line,
		})
	}
	// Cap to 200 most recent.
	if len(filtered) > 200 {
		filtered = filtered[len(filtered)-200:]
	}

	renderPage(w, "logs.html", logsData{
		pageData: pageData{Active: "logs"},
		Entries:  filtered,
	}, logger)
}

// handleProviders renders the providers page with a read-only table.
func handleProviders(w http.ResponseWriter, cfg *config.Config, logger *slog.Logger) {
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

	renderPage(w, "providers.html", providersData{
		pageData:  pageData{Active: "providers"},
		Providers: rows,
	}, logger)
}

// handleMappings renders the mappings page with a read-only table.
func handleMappings(w http.ResponseWriter, cfg *config.Config, logger *slog.Logger) {
	mappings := cfg.MappingsSnapshot()

	var rows []mappingRow
	for name, m := range mappings {
		rows = append(rows, mappingRow{
			Name:         name,
			ProviderName: m.ProviderName,
			Model:        m.ModelString,
		})
	}

	renderPage(w, "mappings.html", mappingsData{
		pageData: pageData{Active: "mappings"},
		Mappings: rows,
	}, logger)
}

// parseMinLevel parses a ?min= query parameter into a slog.Level.
// Returns nil for empty/invalid values (no filtering).
func parseMinLevel(s string) *slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		l := slog.LevelDebug
		return &l
	case "info":
		l := slog.LevelInfo
		return &l
	case "warn":
		l := slog.LevelWarn
		return &l
	case "error":
		l := slog.LevelError
		return &l
	default:
		return nil
	}
}

// levelLabel returns a short label for a log level.
func levelLabel(l slog.Level) string {
	switch {
	case l <= slog.LevelDebug:
		return "debug"
	case l <= slog.LevelInfo:
		return "info"
	case l <= slog.LevelWarn:
		return "warn"
	default:
		return "error"
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
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": name})
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": name})
}

func handleDeleteProvider(w http.ResponseWriter, r *http.Request, cfg *config.Config, cfgPath string) {
	name, err := pathName(r, "/v1/providers/")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_path", err.Error())
		return
	}

	cfg.RLock()
	_, existed := cfg.Providers[name]
	if !existed {
		cfg.RUnlock()
		writeJSONError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	// Check if any mapping uses this provider.
	for _, m := range cfg.MappingsSnapshot() {
		if m.ProviderName == name {
			cfg.RUnlock()
			writeJSONError(w, http.StatusConflict, "provider_in_use", "mappings reference this provider")
			return
		}
	}
	cfg.RUnlock()

	cfg.Lock()
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
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
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": name})
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": name})
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

// --- JSON response helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeValidationError(w http.ResponseWriter, ve *ValidationError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(ve)
}
