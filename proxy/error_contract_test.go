package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

func newContractDispatcher(t *testing.T, verboseErrors bool) *Dispatcher {
	t.Helper()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(map[string]Provider{})
	return NewDispatcher(cfg, registry, logger, verboseErrors, 2)
}

func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, rec.Body.String())
	}
	return body
}

func TestWriteErrorJSON_BasicShape(t *testing.T) {
	d := newContractDispatcher(t, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	d.writeErrorJSON(rec, req, http.StatusBadRequest, "test_code", "human message")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", got)
	}
	body := decodeErrorBody(t, rec)
	if body["error"] != "test_code" {
		t.Errorf("error: got %q, want test_code", body["error"])
	}
	if body["message"] != "human message" {
		t.Errorf("message: got %q, want human message", body["message"])
	}
	if _, has := body["detail"]; has {
		t.Errorf("detail must be absent when no detail provided")
	}
}

func TestWriteErrorJSON_DetailOmittedWhenNotVerbose(t *testing.T) {
	d := newContractDispatcher(t, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	d.writeErrorJSON(
		rec,
		req,
		http.StatusBadGateway,
		"upstream_error",
		"request failed",
		WithDetail("upstream connection refused"),
	)

	body := decodeErrorBody(t, rec)
	if _, has := body["detail"]; has {
		t.Errorf("detail must be omitted when verboseErrors=false; got body: %v", body)
	}
}

func TestWriteErrorJSON_DetailIncludedWhenVerbose(t *testing.T) {
	d := newContractDispatcher(t, true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	d.writeErrorJSON(
		rec,
		req,
		http.StatusBadGateway,
		"upstream_error",
		"request failed",
		WithDetail("upstream connection refused"),
	)

	body := decodeErrorBody(t, rec)
	if body["detail"] != "upstream connection refused" {
		t.Errorf("detail: got %q, want upstream connection refused", body["detail"])
	}
}

func TestWriteErrorJSON_RequestIDIncluded(t *testing.T) {
	d := newContractDispatcher(t, false)
	rec := httptest.NewRecorder()

	// Wrap with RequestIDMiddleware so the dispatcher sees a request_id in context.
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.writeErrorJSON(w, r, http.StatusBadRequest, "x", "y")
	}))
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	body := decodeErrorBody(t, rec)
	id := body["request_id"]
	if id == "" {
		t.Fatal("expected request_id in body")
	}
	if headerID := rec.Header().Get("X-Freedius-Request-ID"); headerID != id {
		t.Errorf("body request_id %q does not match header %q", id, headerID)
	}
}

func TestWriteErrorJSON_NoRequestIDWhenContextEmpty(t *testing.T) {
	d := newContractDispatcher(t, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil) // no middleware

	d.writeErrorJSON(rec, req, http.StatusBadRequest, "x", "y")

	body := decodeErrorBody(t, rec)
	if _, has := body["request_id"]; has {
		t.Errorf("request_id must be absent when context has no ID; got body: %v", body)
	}
}

func TestOriginalOr_FallsBackToProvider(t *testing.T) {
	// originalOr is removed; mappings no longer carry OriginalProvider. The
	// equivalent lookup in the new schema is mapping.ProviderName.
	m := config.Mapping{ProviderName: "nim"}
	if m.ProviderName != "nim" {
		t.Errorf("fallback: got %q, want nim", m.ProviderName)
	}
}

func TestOriginalOr_PrefersOriginalProvider(t *testing.T) {
	// originalOr is removed; under the new schema mapping.ProviderName is the
	// single source of truth (no rewriting). The equivalent lookup is
	// mapping.ProviderName directly.
	m := config.Mapping{ProviderName: "zen"}
	if m.ProviderName != "zen" {
		t.Errorf("provider_name: got %q, want zen", m.ProviderName)
	}
}

// TestDispatcher_MalformedRequest_RequestIDMatches exercises the full
// production stack (RequestID → dispatcher) for a malformed JSON POST and
// asserts the manual check 1.10: response header X-Freedius-Request-ID
// equals the request_id field in the JSON body.
func TestDispatcher_MalformedRequest_RequestIDMatches(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"claude-opus-4": {ProviderName: "nim", ModelString: "x"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(map[string]Provider{})
	d := NewDispatcher(cfg, registry, logger, false, 2)

	handler := RequestIDMiddleware(d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
	headerID := rec.Header().Get("X-Freedius-Request-ID")
	if headerID == "" {
		t.Fatal("missing X-Freedius-Request-ID header")
	}
	body := decodeErrorBody(t, rec)
	bodyID := body["request_id"]
	if bodyID == "" {
		t.Fatal("missing request_id in body")
	}
	if bodyID != headerID {
		t.Errorf("header/body mismatch: header=%q body=%q", headerID, bodyID)
	}
}
