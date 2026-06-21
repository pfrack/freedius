//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

// IPCServer serves SSE streams and JSON endpoints over a Unix domain socket
// for the attached TUI to consume.
type IPCServer struct {
	socketPath string
	bus        *proxy.EventBus
	logSink    *proxy.LogSink
	cfg        *config.Config
	registry   *proxy.Registry
	listener   net.Listener
	server     *http.Server
	startTime  time.Time
}

// StatsSnapshot is a JSON-serializable proxy stats summary.
type StatsSnapshot struct {
	Uptime      string `json:"uptime"`
	TotalEvents int    `json:"total_events"`
	TotalLogs   int64  `json:"total_logs"`
	Port        string `json:"port"`
	Host        string `json:"host"`
}

// NewIPCServer creates a new IPC server bound to the given Unix socket path.
func NewIPCServer(
	socketPath string,
	bus *proxy.EventBus,
	logSink *proxy.LogSink,
	cfg *config.Config,
	registry *proxy.Registry,
) *IPCServer {
	return &IPCServer{
		socketPath: socketPath,
		bus:        bus,
		logSink:    logSink,
		cfg:        cfg,
		registry:   registry,
		startTime:  time.Now(),
	}
}

// ListenAndServe starts the Unix socket HTTP server. It handles stale socket
// cleanup on startup and blocks until the server is shut down.
func (s *IPCServer) ListenAndServe() error {
	// Clean up stale socket if present.
	if conn, err := net.Dial("unix", s.socketPath); err == nil { //nolint:gosec // Unix socket path from runtimeDir()
		_ = conn.Close()
		return fmt.Errorf("IPC socket already in use: %s", s.socketPath)
	}
	_ = os.Remove(s.socketPath) //nolint:gosec // Unix socket path from runtimeDir()

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("IPC listen: %w", err)
	}
	if err := os.Chmod(s.socketPath, 0600); err != nil { //nolint:gosec // Unix socket path from runtimeDir()
		_ = ln.Close()
		return fmt.Errorf("IPC chmod: %w", err)
	}

	s.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	mux.HandleFunc("GET /v1/logs", s.handleLogs)
	mux.HandleFunc("GET /v1/stats", s.handleStats)
	mux.HandleFunc("GET /v1/config", s.handleConfig)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s.server.Serve(ln)
}

// Shutdown gracefully shuts down the IPC server and removes the socket file.
func (s *IPCServer) Shutdown(ctx context.Context) error {
	var err error
	if s.server != nil {
		err = s.server.Shutdown(ctx)
	}
	_ = os.Remove(s.socketPath) //nolint:gosec // Unix socket path from runtimeDir()
	return err
}

func (s *IPCServer) handleEvents(w http.ResponseWriter, r *http.Request) {
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
	events, currentSeq, evicted := s.bus.Since(sinceSeq)
	if evicted {
		s.writeSSE(w, "replay", map[string]any{"complete": false, "current_seq": currentSeq})
		flusher.Flush()
	}
	for _, e := range events {
		s.writeSSE(w, "event", e)
		flusher.Flush()
	}
	s.writeSSE(w, "replay", map[string]any{"complete": true, "current_seq": currentSeq})
	flusher.Flush()

	// Live stream: subscribe to new events.
	ch := s.bus.Subscribe()
	seq := currentSeq
	for {
		select {
		case e := <-ch:
			if e.Seq <= seq {
				continue
			}
			seq = e.Seq
			s.writeSSE(w, "event", e)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *IPCServer) handleLogs(w http.ResponseWriter, r *http.Request) {
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
	entries, currentSeq, evicted := s.logSink.SnapshotSince(sinceSeq)
	if evicted {
		s.writeSSE(w, "replay", map[string]any{"complete": false, "current_seq": currentSeq})
		flusher.Flush()
	}
	for _, e := range entries {
		s.writeSSE(w, "log", e)
		flusher.Flush()
	}
	s.writeSSE(w, "replay", map[string]any{"complete": true, "current_seq": currentSeq})
	flusher.Flush()

	// Live stream: subscribe to new logs.
	ch := s.logSink.Subscribe()
	seq := currentSeq
	for {
		select {
		case e := <-ch:
			if e.Seq <= seq {
				continue
			}
			seq = e.Seq
			s.writeSSE(w, "log", e)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *IPCServer) handleStats(w http.ResponseWriter, _ *http.Request) {
	stats := StatsSnapshot{
		Uptime:      time.Since(s.startTime).Round(time.Second).String(),
		TotalEvents: s.bus.EventCount(),
		TotalLogs:   s.logSink.EventCount(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *IPCServer) handleConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.cfg)
}

// writeSSE writes an SSE event to the response writer.
// Uses json.Marshal (NOT json.NewEncoder) per lessons.md §1.
func (s *IPCServer) writeSSE(w http.ResponseWriter, eventType string, data any) {
	buf, err := json.Marshal(data)
	if err != nil {
		slog.Error("IPC SSE marshal error", "err", err)
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, buf)
}
