package web

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/pfrack/freedius/internal/eventstream"
)

// Server provides a self-contained HTTP server for the embedded web UI.
// It binds to a configurable port, serves templates + static assets, and
// mounts SSE/JSON event handlers for live monitoring.
type Server struct {
	host     string
	port     int
	listener net.Listener
	server   *http.Server
	logger   *slog.Logger
}

// NewServer creates a new Server instance. It does NOT start listening
// until ListenAndServe is called.
func NewServer(
	host string,
	port int,
	h *eventstream.Handlers,
	logger *slog.Logger,
) *Server {
	mux := SetupMux(h, logger)

	return &Server{
		host: host,
		port: port,
		server: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		logger: logger,
	}
}

// ListenAndServe binds to the configured port, starts the HTTP server, and
// blocks until the server is shut down or an error occurs.
func (s *Server) ListenAndServe() error {
	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("web listen: %w", err)
	}
	s.listener = ln
	return s.server.Serve(ln)
}

// Shutdown gracefully shuts down the web server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}
