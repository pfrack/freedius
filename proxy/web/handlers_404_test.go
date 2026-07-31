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
	if !strings.Contains(body, `class="stats-grid"`) {
		t.Errorf("dashboard body missing stats-grid marker; got first 200 chars: %q", body[:min(200, len(body))])
	}
	if strings.Contains(body, "not-found__code") {
		t.Error("dashboard should not render the 404 page")
	}
}
