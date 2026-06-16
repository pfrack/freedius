package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

const (
	defaultHost       = "127.0.0.1"
	defaultPort       = 8080
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
)

var allowedHosts = map[string]struct{}{
	"127.0.0.1": {},
	"0.0.0.0":   {},
}

func main() {
	os.Exit(run())
}

func run() int {
	flagConfig := flag.String("config", "", "path to config file (auto-resolved if empty)")
	flagPort := flag.Int("port", 0, "port to listen on (overrides FREEDIUS_PORT; default 8080)")
	flagHost := flag.String("host", "", "host to bind (127.0.0.1 or 0.0.0.0; default 127.0.0.1)")
	flag.Parse()

	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	port := resolveInt(*flagPort, setFlags["port"], "FREEDIUS_PORT", defaultPort)
	if port < 1 || port > 65535 {
		return failf("freedius: invalid --port value: %d (allowed: 1-65535)", port)
	}

	host := defaultHost
	if setFlags["host"] {
		host = *flagHost
	}
	if _, ok := allowedHosts[host]; !ok {
		return failf("freedius: invalid --host value: %s (allowed: 127.0.0.1, 0.0.0.0)", host)
	}

	baseLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(baseLogger)
	logger := baseLogger

	cfgPath, err := resolveConfigPath(*flagConfig)
	if err != nil {
		baseLogger.Error("config path resolution failed", "err", err)
		return 1
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return failf("freedius: %s \u2014 create one or pass --config <path>", err)
		}
		return failf("freedius: %s", err)
	}

	serverLogger := logger.With("component", "server")
	serverLogger.Info(fmt.Sprintf("freedius listening on http://%s", net.JoinHostPort(host, strconv.Itoa(port))), "host", host, "port", port)

	registry := proxy.NewRegistry(map[string]proxy.Provider{
		"custom": proxy.NewCustomAdapter(logger),
	})
	dispatcher := proxy.NewDispatcher(cfg, registry, logger)
	mux := http.NewServeMux()
	mux.Handle("/", dispatcher)

	server := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return failf("freedius: %v", err)
	case <-ctx.Done():
	}

	serverLogger.Info("shutdown signal received")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return failf("freedius: shutdown error: %v", err)
	}
	serverLogger.Info("shutdown complete")
	return 0
}

func failf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	return 1
}

func resolveInt(flagVal int, flagSet bool, envKey string, def int) int {
	if flagSet {
		return flagVal
	}
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func resolveConfigPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}
	for _, name := range []string{"freedius.yaml", "freedius.yml"} {
		candidate := filepath.Join(cwd, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	xdg, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine user config directory: %w", err)
	}
	return filepath.Join(xdg, "freedius", "config.yaml"), nil
}
