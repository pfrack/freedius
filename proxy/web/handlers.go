// Package web provides the embedded web server for the freedius dashboard.
// It bundles static files (CSS, JS) via embed.FS and serves them through
// an embedded HTTP server.
package web

import (
	"log/slog"
	"net/http"

	"github.com/pfrack/freedius/internal/eventstream"
)

// SetupMux builds the HTTP mux for the web server: page handlers, static
// assets, health check, and eventstream routes.
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
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "logs.html", logsData{pageData: pageData{Active: "logs"}}, logger)
	})
	mux.HandleFunc("GET /providers", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "providers.html", providersData{pageData: pageData{Active: "providers"}}, logger)
	})
	mux.HandleFunc("GET /mappings", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "mappings.html", mappingsData{pageData: pageData{Active: "mappings"}}, logger)
	})

	return mux
}
