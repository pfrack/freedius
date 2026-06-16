package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pfrack/freedius/config"
)

const MaxBodyBytes = 10 * 1024 * 1024

type Dispatcher struct {
	Cfg    *config.Config
	Logger *slog.Logger
}

func NewDispatcher(cfg *config.Config, logger *slog.Logger) *Dispatcher {
	if cfg == nil {
		panic("proxy: nil config")
	}
	if logger == nil {
		panic("proxy: nil logger")
	}
	return &Dispatcher{Cfg: cfg, Logger: logger.With("component", "proxy")}
}

func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		d.writeError(w, http.StatusUnsupportedMediaType, "unsupported content type, expected application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			d.writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body too large (limit: %d bytes)", mbe.Limit))
			return
		}
		d.writeError(w, http.StatusBadRequest, fmt.Sprintf("request body unreadable: %v", err))
		return
	}

	if len(body) == 0 {
		d.writeError(w, http.StatusBadRequest, "invalid request body: empty")
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		d.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Model == "" {
		d.writeError(w, http.StatusBadRequest, "invalid request body: missing or empty \"model\" field")
		return
	}

	m, ok := d.Cfg.Models[req.Model]
	if !ok {
		d.Logger.Debug("no match for model", "model", req.Model)
		d.writeJSON(w, http.StatusNotFound, map[string]string{"status": "no_match"})
		return
	}

	d.Logger.Debug("dispatch", "model", req.Model, "provider", m.Provider, "target_model", m.Model)
	w.Header().Set("X-Freedius-Matched-Provider", m.Provider)
	w.Header().Set("X-Freedius-Matched-Model", m.Model)
	d.writeJSON(w, http.StatusNotImplemented, map[string]string{
		"matched_provider": m.Provider,
		"matched_model":    m.Model,
		"status":           "not_implemented",
	})
}

func (d *Dispatcher) writeJSON(w http.ResponseWriter, status int, body map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		d.Logger.Error("response encode failed", "err", err)
	}
}

func (d *Dispatcher) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		d.Logger.Error("response encode failed", "err", err)
	}
}
