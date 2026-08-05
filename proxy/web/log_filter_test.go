package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/proxy"
)

func TestParseMinLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    *slog.Level
		wantErr bool
	}{
		{"", nil, false},
		{"debug", ptrLevel(slog.LevelDebug), false},
		{"info", ptrLevel(slog.LevelInfo), false},
		{"warn", ptrLevel(slog.LevelWarn), false},
		{"error", ptrLevel(slog.LevelError), false},
		{"INFO", ptrLevel(slog.LevelInfo), false},
		{"all", nil, true},     // unknown non-empty → error per plan §2.9
		{"invalid", nil, true}, // unknown non-empty → error
		{"warrn", nil, true},   // realistic typo → error
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseMinLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseMinLevel(%q) err = nil, want non-nil", tt.input)
				}
				if got != nil {
					t.Errorf("parseMinLevel(%q) level = %v, want nil on error", tt.input, *got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMinLevel(%q) err = %v, want nil", tt.input, err)
			}
			if tt.want == nil {
				if got != nil {
					t.Errorf("parseMinLevel(%q) = %v, want nil", tt.input, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseMinLevel(%q) = nil, want %v", tt.input, *tt.want)
			}
			if *got != *tt.want {
				t.Errorf("parseMinLevel(%q) = %v, want %v", tt.input, *got, *tt.want)
			}
		})
	}
}

func ptrLevel(l slog.Level) *slog.Level { return &l }

func TestHandleLogs_NoFilter(t *testing.T) {
	logSink := proxy.NewLogSink(100)
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	_ = logger

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	rec := httptest.NewRecorder()
	handleLogs(rec, req, logSink, slog.New(slog.NewTextHandler(sink{}, nil)))

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Logs") {
		t.Error("response should contain page title")
	}
}

func TestHandleLogs_LevelFilter(t *testing.T) {
	logSink := proxy.NewLogSink(100)

	req := httptest.NewRequest(http.MethodGet, "/logs?min=info", nil)
	rec := httptest.NewRecorder()
	handleLogs(rec, req, logSink, slog.New(slog.NewTextHandler(sink{}, nil)))

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleLogs_InvalidFilter(t *testing.T) {
	logSink := proxy.NewLogSink(100)

	// Invalid min must return 400 + JSON per plan §2.9.
	req := httptest.NewRequest(http.MethodGet, "/logs?min=invalid", nil)
	rec := httptest.NewRecorder()
	handleLogs(rec, req, logSink, slog.New(slog.NewTextHandler(sink{}, nil)))

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"invalid_filter"`) {
		t.Errorf("body should contain error code; got: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestParseOutcomeFilter(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"success", "success"},
		{"error", "error"},
		{"SUCCESS", "success"},
		{"Error", "error"},
		{"invalid", ""},
		{"all", ""},
		{"  success  ", "success"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseOutcomeFilter(tt.input)
			if got != tt.want {
				t.Errorf("parseOutcomeFilter(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseFallbackFilter(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"true", "true"},
		{"false", "false"},
		{"TRUE", "true"},
		{"False", "false"},
		{"invalid", ""},
		{"yes", ""},
		{"  true  ", "true"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseFallbackFilter(tt.input)
			if got != tt.want {
				t.Errorf("parseFallbackFilter(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleLogs_OutcomeFilter(t *testing.T) {
	logSink := proxy.NewLogSink(100)
	// Use a RingHandler to emit log entries into the sink.
	inner := slog.NewTextHandler(sink{}, nil)
	handler := proxy.NewRingHandler(inner, logSink)
	logger := slog.New(handler)

	// Emit an error-level log line.
	logger.Error("request failed: upstream 500")

	req := httptest.NewRequest(http.MethodGet, "/logs?outcome=error", nil)
	rec := httptest.NewRecorder()
	handleLogs(rec, req, logSink, logger)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "request failed") {
		t.Errorf("expected error log line in filtered results; got: %s", body)
	}

	// Now filter for success — error line should NOT appear.
	req2 := httptest.NewRequest(http.MethodGet, "/logs?outcome=success", nil)
	rec2 := httptest.NewRecorder()
	handleLogs(rec2, req2, logSink, logger)
	body2 := rec2.Body.String()
	if strings.Contains(body2, "request failed") {
		t.Errorf("error log line should not appear with outcome=success; got: %s", body2)
	}
}

func TestHandleLogs_FallbackFilter(t *testing.T) {
	logSink := proxy.NewLogSink(100)
	// Use a RingHandler to emit log entries into the sink.
	inner := slog.NewTextHandler(sink{}, nil)
	handler := proxy.NewRingHandler(inner, logSink)
	logger := slog.New(handler)

	// Emit a log line containing "fallback".
	logger.Info("fallback succeeded: using secondary provider")

	req := httptest.NewRequest(http.MethodGet, "/logs?fallback=true", nil)
	rec := httptest.NewRecorder()
	handleLogs(rec, req, logSink, logger)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "fallback succeeded") {
		t.Errorf("expected fallback log line in filtered results; got: %s", body)
	}

	// Now filter for fallback=false — fallback line should NOT appear.
	req2 := httptest.NewRequest(http.MethodGet, "/logs?fallback=false", nil)
	rec2 := httptest.NewRecorder()
	handleLogs(rec2, req2, logSink, logger)
	body2 := rec2.Body.String()
	if strings.Contains(body2, "fallback succeeded") {
		t.Errorf("fallback log line should not appear with fallback=false; got: %s", body2)
	}
}
