package eventstream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuth_NoTokenConfigured(t *testing.T) {
	h := &Handlers{AuthToken: ""}

	called := false
	handler := h.requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler should be called when no token configured")
	}
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAuth_CorrectToken(t *testing.T) {
	h := &Handlers{AuthToken: "my-secret"}

	called := false
	handler := h.requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer my-secret")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler should be called with correct token")
	}
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAuth_WrongToken(t *testing.T) {
	h := &Handlers{AuthToken: "my-secret"}

	handler := h.requireAuth(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called with wrong token")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "unauthorized") {
		t.Error("response should contain unauthorized error")
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestAuth_MissingHeader(t *testing.T) {
	h := &Handlers{AuthToken: "my-secret"}

	handler := h.requireAuth(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called without auth header")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unauthorized") {
		t.Error("response should contain unauthorized error")
	}
}
