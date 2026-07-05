// Package web provides the embedded web server for the freedius dashboard.
// It bundles static files (CSS, JS) via embed.FS and serves them through
// an embedded HTTP server.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
)

//go:embed templates static
var assets embed.FS

// pageTemplates caches one *template.Template per page file. The layout
// defines `{{block "content" .}}` and each page overrides it, so pages
// can't share a single template set — each page gets its own parsed set
// (layout + that page only). Templates are parsed once at first render
// and reused for the lifetime of the process. Per the plan's Performance
// section: "Template parsing: at startup, once; cached in *template.Template
// (avoid per-request ParseFiles)."
var pageTemplates sync.Map // map[string]*template.Template

// fragmentTemplates caches self-contained fragment templates (no layout).
var fragmentTemplates sync.Map // map[string]*template.Template

// loadFragmentTemplate parses a single template file from the embedded FS
// without wrapping it in layout.html. Used for htmx fragments that replace
// a target div inline (e.g. model list fragment).
func loadFragmentTemplate(name string) (*template.Template, error) {
	if cached, ok := fragmentTemplates.Load(name); ok {
		return cached.(*template.Template), nil
	}
	tmpl, err := template.New(name).ParseFS(assets, "templates/"+name)
	if err != nil {
		return nil, fmt.Errorf("parse fragment %s: %w", name, err)
	}
	actual, _ := fragmentTemplates.LoadOrStore(name, tmpl)
	return actual.(*template.Template), nil
}

// loadPageTemplate parses layout.html + a single page template together
// and caches the result keyed by page file name. Each page defines its own
// `{{define "content"}}` block (which overrides the layout's `{{block
// "content"}}`), so pages must be parsed separately — they can't share a
// single-template set.
func loadPageTemplate(pageFile string, extraFiles ...string) (*template.Template, error) {
	if cached, ok := pageTemplates.Load(pageFile); ok {
		return cached.(*template.Template), nil
	}
	files := []string{"templates/layout.html", "templates/" + pageFile}
	for _, f := range extraFiles {
		files = append(files, "templates/"+f)
	}
	tmpl, err := template.ParseFS(assets, files...)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", pageFile, err)
	}
	actual, _ := pageTemplates.LoadOrStore(pageFile, tmpl)
	return actual.(*template.Template), nil
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
func renderPage(w http.ResponseWriter, pageFile string, data any, logger *slog.Logger, extraFiles ...string) {
	tmpl, err := loadPageTemplate(pageFile, extraFiles...)
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
