// Package eventstream provides transport-agnostic SSE/JSON handlers for
// the event bus and log sink. Used by both the Unix-socket IPC server (until
// Phase 4) and the new web server.
package eventstream

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

// Handlers is a transport-agnostic set of SSE/JSON handlers that mount on any
// *http.ServeMux. Used by both the Unix-socket IPC server (until Phase 4) and
// the new web server. Preserves lessons.md §1: json.Marshal only, never json.NewEncoder.
type Handlers struct {
	Bus       *proxy.EventBus
	LogSink   *proxy.LogSink
	Cfg       *config.Config
	Registry  *proxy.Registry
	Host      string
	Port      int
	StartTime time.Time
	AuthToken string
	CfgPath   string
}

// Register mounts the four event-stream routes (GET /v1/events, GET /v1/logs,
// GET /v1/stats, GET /v1/config) on the given mux. When AuthToken != "", all
// routes are wrapped by requireAuth middleware (constant-time compare).
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/events", h.requireAuth(h.handleEvents))
	mux.HandleFunc("GET /v1/logs", h.requireAuth(h.handleLogs))
	mux.HandleFunc("GET /v1/stats", h.requireAuth(h.handleStats))
	mux.HandleFunc("GET /v1/config", h.requireAuth(h.handleConfig))
}

// requireAuth wraps a HandlerFunc with optional token authentication. When
// AuthToken is zero-length, all requests pass through. Otherwise, the
// Authorization header is compared using crypto/subtle.ConstantTimeCompare
// against two acceptable forms: "Bearer <token>" and "<token>". Accepting
// both lets clients send either shape without a non-constant-time prefix
// check (compare the whole header constant-time rather than byte-slice ==).
// On mismatch, a 401 JSON error is returned.
func (h *Handlers) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	if h.AuthToken == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("Authorization")
		token := provided
		if strings.HasPrefix(provided, "Bearer ") {
			token = provided[7:]
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(h.AuthToken)) == 1 {
			next(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","message":"invalid or missing token"}`))
	}
}

// RequireAuth wraps any http.Handler with the same token authentication as
// requireAuth but returns an http.Handler suitable for wrapping an entire mux
// (or any subtree). Used by proxy/web.Server to gate all routes — pages,
// writeback, and eventstream — behind one auth boundary when AuthToken is set.
func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	if h.AuthToken == "" {
		return next
	}
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	var sinceSeq int64
	if v := r.URL.Query().Get("since"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &sinceSeq)
	}

	// Replay buffered events.
	events, currentSeq, evicted := h.Bus.Since(sinceSeq)
	if evicted {
		h.writeSSE(w, "replay", map[string]any{"complete": false, "current_seq": currentSeq})
		flusher.Flush()
	}
	for _, e := range events {
		h.writeSSE(w, "event", e)
		flusher.Flush()
	}
	h.writeSSE(w, "replay", map[string]any{"complete": true, "current_seq": currentSeq})
	flusher.Flush()

	// Live stream: subscribe to new events.
	ch := h.Bus.Subscribe()
	seq := currentSeq
	for {
		select {
		case e := <-ch:
			if e.Seq <= seq {
				continue
			}
			seq = e.Seq
			h.writeSSE(w, "event", e)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handlers) handleLogs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	var sinceSeq int64
	if v := r.URL.Query().Get("since"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &sinceSeq)
	}

	// Replay buffered logs.
	entries, currentSeq, evicted := h.LogSink.SnapshotSince(sinceSeq)
	if evicted {
		h.writeSSE(w, "replay", map[string]any{"complete": false, "current_seq": currentSeq})
		flusher.Flush()
	}
	for _, e := range entries {
		// Pre-render HTML for the log entry.
		htmlLine := fmt.Sprintf(
			`<pre class="log-%s">%s</pre>`,
			html.EscapeString(levelLabel(e.Level)),
			html.EscapeString(e.Line),
		)
		h.writeSSE(w, "log", htmlLine)
		flusher.Flush()
	}
	h.writeSSE(w, "replay", map[string]any{"complete": true, "current_seq": currentSeq})
	flusher.Flush()

	// Live stream: subscribe to new logs.
	ch := h.LogSink.Subscribe()
	seq := currentSeq
	for {
		select {
		case e := <-ch:
			if e.Seq <= seq {
				continue
			}
			seq = e.Seq
			// Pre-render HTML for the log entry.
			htmlLine := fmt.Sprintf(
				`<pre class="log-%s">%s</pre>`,
				html.EscapeString(levelLabel(e.Level)),
				html.EscapeString(e.Line),
			)
			h.writeSSE(w, "log", htmlLine)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handlers) handleStats(w http.ResponseWriter, _ *http.Request) {
	stats := map[string]any{
		"uptime":       time.Since(h.StartTime).Round(time.Second).String(),
		"total_events": h.Bus.EventCount(),
		"total_logs":   h.LogSink.EventCount(),
		"port":         strconv.Itoa(h.Port),
		"host":         h.Host,
	}
	w.Header().Set("Content-Type", "application/json")
	buf, _ := json.Marshal(stats)
	_, _ = w.Write(buf)
}

func (h *Handlers) handleConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	buf, _ := json.Marshal(h.Cfg)
	_, _ = w.Write(buf)
}

// writeSSE writes an SSE event to the response writer.
// Uses json.Marshal (NOT json.NewEncoder) per lessons.md §1.
func (h *Handlers) writeSSE(w http.ResponseWriter, eventType string, data any) {
	buf, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "SSE marshal error", http.StatusInternalServerError)
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, buf)
	w.(http.Flusher).Flush()
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
