//go:build windows

package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
)

// waitForShutdown blocks until a shutdown signal (SIGINT) is received, then
// gracefully shuts down the server. On Windows, SIGTERM/SIGHUP don't exist,
// so only os.Interrupt is handled. The cleanup arg is discarded — Windows
// has no IPC server (no Unix socket) in this change.
func waitForShutdown(server *http.Server, _ func() error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}
