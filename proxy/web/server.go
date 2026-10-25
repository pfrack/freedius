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

	// Middleware layering (outer → inner):
	//   Auth → StripPrefix → csrfGuard → mux
	//
	// - csrfGuard MUST see the stripped path so its `strings.HasPrefix(r.URL.Path,
	//   "/v1/")` check works when a base-path prefix is set. If csrfGuard sat
	//   outside StripPrefix, prefixed mutation requests like
	//   /freedius/v1/providers/x/test would bypass CSRF entirely.
	// - Auth sits outermost so it applies to the prefixed path (path-agnostic
	//   in practice — it checks the Authorization header — but kept outside for
	//   conceptual clarity).
	// - StripPrefix rewrites r.URL.Path before the inner handlers see it, so
	//   Go 1.22 route patterns remain unchanged.
	handler := csrfGuard(http.Handler(mux))
	if h.BasePath != "" {
		handler = http.StripPrefix(h.BasePath, handler)
	}
	if h.AuthToken != "" {
		handler = h.RequireAuth(handler)
	}

	return &Server{
		host: host,
		port: port,
		server: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			// No WriteTimeout/IdleTimeout for the web server: long-lived SSE
			// connections (/v1/events, /v1/logs) hold the writer open for the
			// lifetime of the subscription. Setting WriteTimeout would close
			// live SSE streams mid-event. The proxy server can afford short
			// timeouts because it doesn't stream; the dashboard cannot.
			// ReadHeaderTimeout is still set (defends against slowloris
			// at the header-read boundary) — that's the load-bearing
			// hardening for this listener.
		},
		logger: logger,
	}
}

// Listen binds to the configured port without serving. Pair with Serve to
// start accepting connections. Separating the two lets callers fail fast on
// bind conflicts (mirrors the proxy's waitForBind pattern): if the dashboard
// port is taken the process exits non-zero rather than running headlessly
// with a silently-dead UI.
func (s *Server) Listen() error {
	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("web listen: %w", err)
	}
	s.listener = ln
	return nil
}

// Serve accepts connections on the bound listener. Blocks until the server
// shuts down or returns an error. Listen MUST be called first.
func (s *Server) Serve() error {
	if s.listener == nil {
		return fmt.Errorf("web serve: listener not bound (Listen not called)")
	}
	return s.server.Serve(s.listener)
}

// ListenAndServe binds and serves in one call. Convenience for tests; main.go
// uses Listen + Serve separately so the bind failure can fail fast.
func (s *Server) ListenAndServe() error {
	if err := s.Listen(); err != nil {
		return err
	}
	return s.Serve()
}

// Shutdown gracefully shuts down the web server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}
