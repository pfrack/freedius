package web

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/pfrack/freedius/internal/eventstream"
)

func TestWebServerLifecycle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	logger := slog.New(slog.NewTextHandler(sink{}, nil))
	h := &eventstream.Handlers{}
	ws := NewServer("127.0.0.1", port, h, logger)

	errCh := make(chan error, 1)
	go func() {
		errCh <- ws.ListenAndServe()
	}()

	// Wait for server to be ready.
	for i := 0; i < 50; i++ {
		resp, err := http.Get("http://127.0.0.1:" + fmt.Sprintf("%d", port) + "/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		if i == 49 {
			t.Fatalf("server did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify health endpoint.
	resp, err := http.Get("http://127.0.0.1:" + fmt.Sprintf("%d", port) + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}

	// Shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ws.Shutdown(ctx); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}
