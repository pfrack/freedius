package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestForwardUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	resp, err := http.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	rec := httptest.NewRecorder()
	if err := forwardUpstreamError(rec, resp); err != nil {
		t.Fatalf("forwardUpstreamError returned err: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("X-Upstream"); got != "yes" {
		t.Errorf("X-Upstream header: got %q, want yes", got)
	}
	if !strings.Contains(rec.Body.String(), "bad key") {
		t.Errorf("body should contain upstream payload, got %q", rec.Body.String())
	}
}

func TestFreediusErrorHandler_TransportError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Run("verbose=false omits detail", func(t *testing.T) {
		handler := freediusErrorHandler(logger, false)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		handler(rec, req, errors.New("connection refused"))

		if rec.Code != http.StatusBadGateway {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadGateway)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["error"] != "upstream_unreachable" {
			t.Errorf("error: got %q, want upstream_unreachable", body["error"])
		}
		if _, ok := body["detail"]; ok {
			t.Errorf("detail must be omitted when verboseErrors=false; got %q", body["detail"])
		}
	})
	t.Run("verbose=true includes detail", func(t *testing.T) {
		handler := freediusErrorHandler(logger, true)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		handler(rec, req, errors.New("connection refused"))

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["detail"] != "connection refused" {
			t.Errorf("detail: got %q, want connection refused", body["detail"])
		}
	})
}

func TestFreediusErrorHandler_ClientCanceled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := freediusErrorHandler(logger, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	handler(rec, req, context.Canceled)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (no write on cancel)", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body should be empty on cancel, got %q", rec.Body.String())
	}
}
