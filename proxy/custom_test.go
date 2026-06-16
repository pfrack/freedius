package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

type failingRecorder struct {
	*httptest.ResponseRecorder
	failed bool
}

func (fr *failingRecorder) Write(data []byte) (int, error) {
	if !fr.failed {
		fr.failed = true
		return 0, io.ErrShortWrite
	}
	return fr.ResponseRecorder.Write(data)
}

func newCustomTestAdapter(t *testing.T) *CustomAdapter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewCustomAdapter(logger)
}

func TestCustomAdapter_PassthroughText(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization: got %q, want Bearer sk-test", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"my-shim"`) {
			t.Errorf("upstream did not receive original body, got %q", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"my-shim"}`)))
	a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: upstream.URL, APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{"model":"my-shim"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("body: got %q", rec.Body.String())
	}
}

func TestCustomAdapter_PassthroughStreaming(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Write three SSE chunks
		_, _ = w.Write([]byte("data: {\"id\":\"1\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"2\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"3\"}\n\n"))
	}))
	defer upstream.Close()

	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"my-shim"}`)))
	body := []byte(`{"model":"my-shim"}`)
	if err := a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: upstream.URL, APIKeyEnv: "CUSTOM_API_KEY"}, body); err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	respBody := rec.Body.String()
	for _, id := range []string{"1", "2", "3"} {
		if !strings.Contains(respBody, `"id":"`+id+`"`) {
			t.Errorf("missing chunk id %s in %q", id, respBody)
		}
	}
}

func TestCustomAdapter_Upstream401(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"my-shim"}`)))
	a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: upstream.URL, APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{"model":"my-shim"}`))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "bad key") {
		t.Errorf("body: got %q", rec.Body.String())
	}
}

func TestCustomAdapter_Upstream500(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`oops`))
	}))
	defer upstream.Close()

	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: upstream.URL, APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{}`))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestCustomAdapter_MissingEnvVar(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "")
	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: "https://example.com", APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
	if !strings.Contains(err.Error(), "CUSTOM_API_KEY") {
		t.Errorf("error should mention env var: %v", err)
	}
}

func TestCustomAdapter_MissingBaseURL(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	a := newCustomTestAdapter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	err := a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestCustomAdapter_ClientDisconnect(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "sk-test")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	rec := &failingRecorder{ResponseRecorder: httptest.NewRecorder(), failed: false}

	a := newCustomTestAdapter(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"my-shim"}`)))
	err := a.Handle(rec, req, config.Model{Provider: "custom", Model: "my-shim", BaseURL: upstream.URL, APIKeyEnv: "CUSTOM_API_KEY"}, []byte(`{"model":"my-shim"}`))
	if err != nil {
		t.Fatalf("Handle returned %v, want nil", err)
	}
}
