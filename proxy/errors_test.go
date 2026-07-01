package proxy

import (
	"bytes"
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
	t.Run("emits Anthropic-format 529", func(t *testing.T) {
		handler := freediusErrorHandler(logger, false)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		handler(rec, req, errors.New("connection refused"))

		if rec.Code != 529 {
			t.Errorf("status: got %d, want 529", rec.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["type"] != "error" {
			t.Errorf("type: got %v, want error", body["type"])
		}
		inner := body["error"].(map[string]any)
		if inner["type"] != "overloaded_error" {
			t.Errorf("error.type: got %v, want overloaded_error", inner["type"])
		}
		if got := rec.Header().Get("retry-after"); got != "15" {
			t.Errorf("retry-after: got %q, want 15", got)
		}
		if got := rec.Header().Get("x-should-retry"); got != "true" {
			t.Errorf("x-should-retry: got %q, want true", got)
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
		{"502", 502, "", 502, "api_error", "15", false},
		{"504", 504, "", 504, "api_error", "15", false},
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

func TestTranslateUpstreamError_LargeBody(t *testing.T) {
	bigBody := strings.Repeat("x", 1024)
	resp := &http.Response{
		StatusCode: 500,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(bigBody)),
	}
	rec := httptest.NewRecorder()
	translateUpstreamError(rec, resp)

	if rec.Code != 500 {
		t.Errorf("status: got %d, want 500", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	inner := body["error"].(map[string]any)
	msg := inner["message"].(string)
	if len(msg) > 256 {
		t.Errorf("message length: got %d, want ≤ 256", len(msg))
	}
	if !strings.Contains(msg, strings.Repeat("x", 100)) {
		t.Errorf("message should contain start of upstream body, got %q", msg)
	}
}

func TestTranslateUpstreamError_BinaryBody(t *testing.T) {
	binaryBody := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd, 'o', 'k', 0x00, 0x03}
	resp := &http.Response{
		StatusCode: 502,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(binaryBody)),
	}
	rec := httptest.NewRecorder()
	translateUpstreamError(rec, resp)

	if rec.Code != 502 {
		t.Errorf("status: got %d, want 502", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	inner := body["error"].(map[string]any)
	msg := inner["message"].(string)
	// sanitizePrintable strips non-printable chars. Binary bytes become U+FFFD
	// replacement characters (printable) and "ok" is preserved.
	if !strings.Contains(msg, "ok") {
		t.Errorf("message should contain %q, got %q", "ok", msg)
	}
	if len(msg) < 2 {
		t.Errorf("message too short; expected printable chars from binary input, got %q", msg)
	}
}

func TestTranslateUpstreamError_HTMLErrorPage(t *testing.T) {
	htmlBody := `<html><body><h1>Bad Gateway</h1><p>CDN error</p></body></html>`
	resp := &http.Response{
		StatusCode: 502,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(htmlBody)),
	}
	rec := httptest.NewRecorder()
	translateUpstreamError(rec, resp)

	if rec.Code != 502 {
		t.Errorf("status: got %d, want 502", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	inner := body["error"].(map[string]any)
	msg := inner["message"].(string)
	// sanitizePrintable keeps HTML tags (they're printable chars).
	if !strings.Contains(msg, "Bad Gateway") {
		t.Errorf("message should contain HTML body content, got %q", msg)
	}
	if inner["type"] != "api_error" {
		t.Errorf("error.type: got %v, want api_error", inner["type"])
	}
}

func TestTranslateUpstreamError_EmptyBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: 429,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	rec := httptest.NewRecorder()
	translateUpstreamError(rec, resp)

	if rec.Code != 429 {
		t.Errorf("status: got %d, want 429", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	inner := body["error"].(map[string]any)
	if inner["type"] != "rate_limit_error" {
		t.Errorf("error.type: got %v, want rate_limit_error", inner["type"])
	}
	// Empty body must produce empty message, not a trailing-space artifact.
	if msg := inner["message"].(string); msg != "" {
		t.Errorf("message: got %q, want empty string", msg)
	}
}
