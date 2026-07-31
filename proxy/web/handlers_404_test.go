package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnknownPath_ReturnsBranded404(t *testing.T) {
	mux := newTestMux()

	req := httptest.NewRequest(http.MethodGet, "/definitely-not-a-route", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, marker := range []string{
		"not-found__code",
		"Page not found",
		`href="/"`,
		"Back to dashboard",
		`<a href="/mappings"`,
		`<a href="/providers"`,
		`<a href="/logs"`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("404 body missing marker %q", marker)
		}
	}
	if !strings.Contains(body, "<nav>") {
		t.Error("404 page should render the sidebar <nav>")
	}
}

func TestRootDashboardStillReturns200(t *testing.T) {
	mux := newTestMux()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="health-strip"`) {
		t.Errorf("dashboard body missing health-strip marker; got first 200 chars: %q", body[:min(200, len(body))])
	}
	if strings.Contains(body, "not-found__code") {
		t.Error("dashboard should not render the 404 page")
	}
}

func TestMissingStaticAsset_ReturnsBranded404(t *testing.T) {
	mux := newTestMux()

	req := httptest.NewRequest(http.MethodGet, "/static/does-not-exist.css", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("missing static asset should render the branded 404 HTML page")
	}
	if strings.Contains(body, "404 page not found") {
		t.Error("missing static asset should NOT use the FileServer's plain-text 404 body")
	}
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "max-age=300") {
		t.Errorf("missing-asset 404 should not carry public, max-age=300; got %q", cc)
	}
}

func TestExistingStaticAsset_StillServedWithCacheHeader(t *testing.T) {
	mux := newTestMux()

	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=300") {
		t.Errorf("Cache-Control = %q, want max-age=300", cc)
	}
	if strings.Contains(rec.Body.String(), "<!DOCTYPE html>") {
		t.Error("real static asset should not render the 404 HTML page")
	}
}
