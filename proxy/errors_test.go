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

func TestWriteAnthropicError(t *testing.T) {
	t.Run("with retry", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeAnthropicError(rec, 429, "rate_limit_error", "slow down", 30)

		if rec.Code != 429 {
			t.Errorf("status: got %d, want 429", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type: got %q", got)
		}
		if got := rec.Header().Get("retry-after"); got != "30" {
			t.Errorf("retry-after: got %q, want 30", got)
		}
		if got := rec.Header().Get("x-should-retry"); got != "true" {
			t.Errorf("x-should-retry: got %q, want true", got)
		}
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["type"] != "error" {
			t.Errorf("type: got %v, want error", body["type"])
		}
		inner := body["error"].(map[string]any)
		if inner["type"] != "rate_limit_error" {
			t.Errorf("error.type: got %v", inner["type"])
		}
		if inner["message"] != "slow down" {
			t.Errorf("error.message: got %v", inner["message"])
		}
	})
	t.Run("without retry", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeAnthropicError(rec, 401, "authentication_error", "bad key", 0)

		if rec.Code != 401 {
			t.Errorf("status: got %d, want 401", rec.Code)
		}
		if got := rec.Header().Get("retry-after"); got != "" {
			t.Errorf("retry-after should be absent, got %q", got)
		}
		if got := rec.Header().Get("x-should-retry"); got != "" {
			t.Errorf("x-should-retry should be absent, got %q", got)
		}
	})
}

func TestTranslateUpstreamError(t *testing.T) {
	cases := []struct {
		name        string
		statusCode  int
		retryHeader string
		wantStatus  int
		wantErrType string
		wantRetry   string
		wantNoRetry bool
	}{
		{"429 with retry-after", 429, "42", 429, "rate_limit_error", "42", false},
		{"429 no retry-after", 429, "", 429, "rate_limit_error", "15", false},
		{"503", 503, "", 529, "overloaded_error", "15", false},
		{"529", 529, "10", 529, "overloaded_error", "10", false},
		{"500", 500, "", 500, "api_error", "15", false},
		{"401", 401, "", 401, "authentication_error", "", true},
		{"403", 403, "", 401, "authentication_error", "", true},
		{"404", 404, "", 404, "invalid_request_error", "", true},
		{"502", 502, "", 529, "overloaded_error", "15", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tc.statusCode,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("upstream says oops")),
			}
			if tc.retryHeader != "" {
				resp.Header.Set("retry-after", tc.retryHeader)
			}
			rec := httptest.NewRecorder()
			translateUpstreamError(rec, resp)

			if rec.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			var body map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			inner := body["error"].(map[string]any)
			if inner["type"] != tc.wantErrType {
				t.Errorf("error.type: got %v, want %s", inner["type"], tc.wantErrType)
			}
			if tc.wantNoRetry {
				if got := rec.Header().Get("retry-after"); got != "" {
					t.Errorf("retry-after should be absent, got %q", got)
				}
			} else {
				if got := rec.Header().Get("retry-after"); got != tc.wantRetry {
					t.Errorf("retry-after: got %q, want %q", got, tc.wantRetry)
				}
				if got := rec.Header().Get("x-should-retry"); got != "true" {
					t.Errorf("x-should-retry: got %q, want true", got)
				}
			}
			// Message should contain upstream body snippet
			msg := inner["message"].(string)
			if !strings.Contains(msg, "upstream says oops") {
				t.Errorf("message should contain upstream body, got %q", msg)
			}
		})
	}
}
