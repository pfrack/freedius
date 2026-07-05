package web

import (
	"strings"
	"testing"
)

func TestAssets_Open(t *testing.T) {
	f, err := assets.Open("static/htmx.min.js")
	if err != nil {
		t.Fatalf("open static/htmx.min.js: %v", err)
	}
	f.Close()

	f, err = assets.Open("templates/layout.html")
	if err != nil {
		t.Fatalf("open templates/layout.html: %v", err)
	}
	f.Close()
}

func TestLoadPageTemplate(t *testing.T) {
	tmpl, err := loadPageTemplate("index.html")
	if err != nil {
		t.Fatalf("loadPageTemplate: %v", err)
	}

	var buf strings.Builder
	data := indexData{
		pageData:    pageData{Active: "index"},
		Uptime:      "0s",
		TotalEvents: 0,
		TotalLogs:   0,
		Port:        "8080",
		Host:        "localhost",
	}
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatalf("execute layout: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "/static/htmx.min.js") {
		t.Error("rendered layout should reference htmx.min.js")
	}
	if !strings.Contains(out, "/static/app.css") {
		t.Error("rendered layout should reference app.css")
	}
	if !strings.Contains(out, "sidebar") {
		t.Error("rendered layout should contain sidebar")
	}
}
