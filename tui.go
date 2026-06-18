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
	"path/filepath"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
	"github.com/pfrack/freedius/proxy/tui"
)

func runTUI(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	flagConfig := fs.String("config", "", "path to config file (auto-resolved if empty)")
	flagPort := fs.Int("port", 0, "port to listen on (overrides FREEDIUS_PORT; default 8082)")
	flagHost := fs.String("host", "", "host to bind (127.0.0.1 or 0.0.0.0; default 127.0.0.1)")
	flagLogFormat := fs.String(
		"log-format",
		"",
		"log output format: text, json (default text)",
	)
	flagStreamTimeout := fs.Duration(
		"stream-timeout",
		0,
		"per-request upstream timeout (default 5m)",
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: freedius tui [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	logFormat := *flagLogFormat
	if logFormat == "" {
		logFormat = os.Getenv("FREEDIUS_LOG")
	}
	if logFormat == "" {
		logFormat = "text"
	}
	logger, err := newLogger(logFormat, os.Stderr)
	if err != nil {
		return failf("freedius: %v", err)
	}
	slog.SetDefault(logger)

	streamTimeout := defaultStreamTimeout
	if *flagStreamTimeout != 0 {
		streamTimeout = *flagStreamTimeout
	} else if v := os.Getenv("FREEDIUS_STREAM_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			streamTimeout = d
		}
	}

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

	cfgPath, err := resolveConfigPath(*flagConfig)
	if err != nil {
		logger.Error("config path resolution failed", "err", err)
		return 1
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && *flagConfig == "" {
			logger.Info("no config found, writing default config", "path", cfgPath)
			if parent := filepath.Dir(cfgPath); parent != "." {
				// #nosec G301 -- user-owned config directory; group/other read keeps tools compatible
				if err := os.MkdirAll(parent, 0o755); err != nil {
					return failf("freedius: cannot create config directory %s: %v", parent, err)
				}
			}
			// #nosec G306 -- starter config is non-sensitive and should be readable by tooling
			if err := os.WriteFile(cfgPath, []byte(starterTemplate), 0o644); err != nil {
				return failf("freedius: write default config %s: %v", cfgPath, err)
			}
			fmt.Fprintf(os.Stderr, "wrote default config to %s\n", cfgPath)
			cfg, err = config.Load(cfgPath)
			if err != nil {
				return failf("freedius: %s", err)
			}
		} else {
			return failf("freedius: %s", err)
		}
	}

	// In TUI mode, we don't block on missing env vars at startup — the user can
	// see errors in the dashboard when requests fail.
	_ = checkRequiredEnvVars(cfg)

	registry := proxy.NewDefaultRegistry(logger, streamTimeout, false, nil)
	dispatcher := proxy.NewDispatcher(cfg, registry, logger, false)
	bus := proxy.NewEventBus(1000)

	mux := http.NewServeMux()
	httpHandler := proxy.RecoverMiddleware(logger, false, dispatcher)
	httpHandler = proxy.EventBusMiddleware(bus, httpHandler)
	httpHandler = proxy.AccessLogMiddleware(logger, httpHandler)
	httpHandler = proxy.RequestIDMiddleware(httpHandler)
	mux.Handle("/", httpHandler)

	server := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("freedius TUI proxy starting", "host", host, "port", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	model := tui.NewDashboard(bus.Subscribe(), cfg, registry)
	prog := tea.NewProgram(model)
	if _, err := prog.Run(); err != nil {
		logger.Error("TUI program error", "err", err)
	}

	logger.Info("TUI shutdown, stopping proxy")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("proxy shutdown error", "err", err)
	}
	logger.Info("shutdown complete")
	return 0
}
