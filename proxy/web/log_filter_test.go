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
		input string
		want  *slog.Level
	}{
		{"", nil},
		{"all", nil},
		{"invalid", nil},
		{"debug", ptrLevel(slog.LevelDebug)},
		{"info", ptrLevel(slog.LevelInfo)},
		{"warn", ptrLevel(slog.LevelWarn)},
		{"error", ptrLevel(slog.LevelError)},
		{"INFO", ptrLevel(slog.LevelInfo)},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseMinLevel(tt.input)
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
	// Emit some log entries via the ring handler.
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	_ = logger // entries aren't emitted via the logger in this test

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
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleLogs_InvalidFilter(t *testing.T) {
	logSink := proxy.NewLogSink(100)

	// Invalid min value should still render (no filtering applied).
	req := httptest.NewRequest(http.MethodGet, "/logs?min=invalid", nil)
	rec := httptest.NewRecorder()
	handleLogs(rec, req, logSink, slog.New(slog.NewTextHandler(sink{}, nil)))

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
