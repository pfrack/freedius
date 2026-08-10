package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pfrack/freedius/proxy"
)

// pushLogs emits the given messages at INFO level through the ring handler so
// handleLogs can filter them, returning the populated sink.
func pushLogs(t *testing.T, messages ...string) *proxy.LogSink {
	t.Helper()
	logSink := proxy.NewLogSink(100)
	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	ringHandler := proxy.NewRingHandler(logger.Handler(), logSink)
	logger = slog.New(ringHandler)
	for _, m := range messages {
		logger.Info(m)
	}
	return logSink
}

// TestHandleLogs_FilterParity asserts the server-side predicate honors all
// five filter dimensions (provider / mapping / level-min / outcome /
// fallback), guarding against the regression the SSE live tail used to leak.
func TestHandleLogs_FilterParity(t *testing.T) {
	logSink := pushLogs(t,
		"dispatch openai via model gpt-4",
		"dispatch anthropic via model claude",
	)
	// Error-level line (contains "fallback") — must match level/outcome/fallback.
	errLogger := slog.New(slog.NewTextHandler(sink{}, nil))
	errRing := proxy.NewRingHandler(errLogger.Handler(), logSink)
	slog.New(errRing).Error("calling fallback now")

	run := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, "/logs"+query, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		handleLogs(rec, req, logSink, slog.New(slog.NewTextHandler(sink{}, nil)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	// Provider substring (case-insensitive).
	if b := run("?provider=OPENAI"); !strings.Contains(b, "openai") || strings.Contains(b, "anthropic") {
		t.Errorf("provider filter leaked; body: %s", b)
	}
	if b := run("?provider=anthropic"); !strings.Contains(b, "anthropic") {
		t.Errorf("provider filter dropped its match; body: %s", b)
	}

	// Mapping substring.
	if b := run("?mapping=claude"); !strings.Contains(b, "claude") || strings.Contains(b, "gpt-4") {
		t.Errorf("mapping filter leaked; body: %s", b)
	}

	// Level min: only the error line survives min=error; info lines drop.
	if b := run("?min=error"); !strings.Contains(b, "calling fallback now") {
		t.Errorf("min=error should keep the error line; body: %s", b)
	}
	if b := run("?min=error"); strings.Contains(b, "gpt-4") {
		t.Errorf("min=error should drop info lines; body: %s", b)
	}

	// Outcome error: only the error line.
	if b := run("?outcome=error"); !strings.Contains(b, "calling fallback now") || strings.Contains(b, "gpt-4") {
		t.Errorf("outcome=error leaked non-error lines; body: %s", b)
	}
	// Outcome success: error line excluded.
	if b := run("?outcome=success"); strings.Contains(b, "calling fallback now") {
		t.Errorf("outcome=success should drop the error line; body: %s", b)
	}

	// Fallback true: only the line containing "fallback".
	if b := run("?fallback=true"); !strings.Contains(b, "fallback") || strings.Contains(b, "gpt-4") {
		t.Errorf("fallback=true leaked non-fallback lines; body: %s", b)
	}
	// Fallback false: error/fallback line excluded.
	if b := run("?fallback=false"); strings.Contains(b, "calling fallback now") {
		t.Errorf("fallback=false should drop the fallback line; body: %s", b)
	}
}
