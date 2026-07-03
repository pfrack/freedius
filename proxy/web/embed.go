// Package web provides the embedded web server for the freedius dashboard.
// It bundles static files (CSS, JS) via embed.FS and serves them through
// an embedded HTTP server.
package web

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"text/template"
)

//go:embed templates static
var assets embed.FS

// loadPageTemplate parses layout.html and a single page template together.
// Each page gets its own template set so {{define "content"}} blocks don't
// collide across pages.
func loadPageTemplate(pageFile string) (*template.Template, error) {
	tmpl, err := template.ParseFS(assets, "templates/layout.html", "templates/"+pageFile)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", pageFile, err)
	}
	return tmpl, nil
}

// StaticFS returns the embedded static/ directory for serving static assets.
func StaticFS() fs.FS {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// serveStatic serves files from the embedded static directory with caching.
func serveStatic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.StripPrefix("/static/", http.FileServerFS(StaticFS())).ServeHTTP(w, r)
}

// renderPage loads the layout + a page template and executes the layout.
func renderPage(w http.ResponseWriter, pageFile string, data any, logger *slog.Logger) {
	tmpl, err := loadPageTemplate(pageFile)
	if err != nil {
		logger.Error("template load failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		logger.Error("template execute failed", "err", err)
	}
}
