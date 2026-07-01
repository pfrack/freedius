//go:build !windows

package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// waitForShutdown blocks until a shutdown signal (SIGINT or SIGTERM) is
// received, then gracefully shuts down the server and runs cleanup.
func waitForShutdown(server *http.Server, cleanup func() error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	if cleanup != nil {
		if err := cleanup(); err != nil {
			return err
		}
	}

	return nil
}
