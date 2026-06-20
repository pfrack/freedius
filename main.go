// Package main implements the freedius binary: a single static executable that
// always starts the TUI dashboard together with the proxy server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "embed"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/envinject"
	"github.com/pfrack/freedius/proxy"
	"github.com/pfrack/freedius/proxy/tui"
)

const (
	defaultHost          = "127.0.0.1"
	defaultPort          = 8082
	shutdownTimeout      = 5 * time.Second
	readHeaderTimeout    = 5 * time.Second
	readTimeout          = 30 * time.Second
	idleTimeout          = 120 * time.Second
	defaultStreamTimeout = 5 * time.Minute
)

var allowedHosts = map[string]struct{}{
	"127.0.0.1": {},
	"0.0.0.0":   {},
}

var version = "dev"

//go:embed templates/starter.yaml
var starterTemplate string

// newLogger constructs the process-wide logger. When format is "json", the
// handler emits structured JSON lines; otherwise it emits the human-readable
// text format.
func newLogger(format string, w io.Writer, sink *proxy.LogSink) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var inner slog.Handler
	switch format {
	case "json":
		inner = slog.NewJSONHandler(w, opts)
	case "text":
		inner = slog.NewTextHandler(w, opts)
	default:
		return nil, fmt.Errorf("invalid log format %q (allowed: text, json)", format)
	}
	handler := proxy.NewRingHandler(inner, sink)
	return slog.New(handler), nil
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is the unified entry point: freedius always starts the TUI+proxy, with
// no subcommand dispatch. --version and --help are handled before flag parsing.
func run(args []string) int {
	// Short-circuit --version / --help so they work even without valid flags.
	for _, a := range args {
		if a == "--version" {
			fmt.Printf("freedius %s\n", version)
			return 0
		}
		if a == "--help" || a == "-h" {
			printUsage(os.Stderr)
			return 0
		}
	}

	fs := flag.NewFlagSet("freedius", flag.ContinueOnError)
	flagConfig := fs.String("config", "", "path to config file (auto-resolved if empty)")
	flagConfigShorthand := fs.String("c", "", "shorthand for --config")
	flagPort := fs.Int("port", 0, "port to listen on (overrides FREEDIUS_PORT; default 8082)")
	flagHost := fs.String("host", "", "host to bind (127.0.0.1 or 0.0.0.0; default 127.0.0.1)")
	flagVerboseErrors := fs.Bool(
		"verbose-errors",
		false,
		"include upstream error detail in error responses (or set FREEDIUS_VERBOSE_ERRORS=1)",
	)
	flagLogFormat := fs.String(
		"log-format",
		"",
		"log output format: text, json (overrides FREEDIUS_LOG; default text)",
	)
	flagStreamTimeout := fs.Duration(
		"stream-timeout",
		0,
		"per-request upstream timeout (overrides FREEDIUS_STREAM_TIMEOUT; default 5m)",
	)
	flagNoExportHint := fs.Bool("no-export-hint", false, "suppress the env-export hint on startup")
	fs.Usage = func() { printUsage(os.Stderr) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	verboseErrors := *flagVerboseErrors || os.Getenv("FREEDIUS_VERBOSE_ERRORS") == "1"

	logFormat := *flagLogFormat
	if logFormat == "" {
		logFormat = os.Getenv("FREEDIUS_LOG")
	}
	if logFormat == "" {
		logFormat = "text"
	}
	logSink := proxy.NewLogSink(1000)
	logger, err := newLogger(logFormat, io.Discard, logSink)
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

	configFlag := *flagConfig
	if configFlag == "" {
		configFlag = *flagConfigShorthand
	}
	cfgPath, err := resolveConfigPath(configFlag)
	if err != nil {
		logger.Error("config path resolution failed", "err", err)
		return 1
	}
	cfg, err := loadConfig(cfgPath, configFlag)
	if err != nil {
		return failf("freedius: %s", err)
	}

	// In unified mode, missing env vars are surfaced to the user via the TUI
	// Config tab rather than blocking startup.
	_ = checkRequiredEnvVars(cfg)

	serverLogger := logger.With("component", "server")
	serverLogger.Info(
		fmt.Sprintf("freedius listening on http://%s", net.JoinHostPort(host, strconv.Itoa(port))),
		"host",
		host,
		"port",
		port,
	)

	if !*flagNoExportHint {
		fmt.Fprintln(os.Stderr, envinject.Snippet(host, port))
	}

	registry := proxy.NewDefaultRegistry(logger, streamTimeout, verboseErrors, nil)
	dispatcher := proxy.NewDispatcher(cfg, registry, logger, verboseErrors)
	bus := proxy.NewEventBus(1000)

	httpHandler := proxy.RecoverMiddleware(logger, verboseErrors, dispatcher)
	httpHandler = proxy.EventBusMiddleware(bus, httpHandler)
	httpHandler = proxy.AccessLogMiddleware(logger, httpHandler)
	httpHandler = proxy.RequestIDMiddleware(httpHandler)

	mux := newMux(httpHandler)

	server := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	if err := waitForBind(serverErr); err != nil {
		return failf("freedius: %v", err)
	}

	model := tui.NewDashboard(
		bus.Subscribe(),
		logSink.Subscribe(),
		cfg, registry, dispatcher, cfgPath, host, port, verboseErrors,
		"",
	)
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

	select {
	case err := <-serverErr:
		return failf("freedius: %v", err)
	default:
	}
	return 0
}

func printUsage(w io.Writer) {
	usage := `freedius — local Claude Code proxy

Usage: freedius [flags]

Flags:
`
	if _, err := io.WriteString(w, usage); err != nil {
		return
	}
	// Print the same defaults as the flag set above. We rebuild a FlagSet so
	// fs.PrintDefaults reflects the canonical flag declarations.
	fs := flag.NewFlagSet("freedius", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.String("config", "", "path to config file (auto-resolved if empty)")
	fs.String("c", "", "shorthand for --config")
	fs.Int("port", 0, "port to listen on (overrides FREEDIUS_PORT; default 8082)")
	fs.String("host", "", "host to bind (127.0.0.1 or 0.0.0.0; default 127.0.0.1)")
	fs.Bool("verbose-errors", false, "include upstream error detail in error responses")
	fs.String("log-format", "", "log output format: text, json (default text)")
	fs.Duration("stream-timeout", 0, "per-request upstream timeout (default 5m)")
	fs.Bool("no-export-hint", false, "suppress the env-export hint on startup")
	fs.PrintDefaults()
}

// waitForBind polls serverErr for up to 50ms looking for an immediate bind
// failure (port in use, permission denied, etc.). Returns the error if one
// arrives in that window, or nil if no error fires — at which point the bind
// has succeeded (or at least has not produced a synchronous error) and the
// caller can proceed to start the TUI.
func waitForBind(serverErr <-chan error) error {
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case err := <-serverErr:
			return err
		case <-time.After(5 * time.Millisecond):
		}
	}
	return nil
}

// loadConfig loads the config from cfgPath, falling back to the embedded
// starter template when the resolved path does not exist and no explicit
// --config flag was passed (lazy startup).
func loadConfig(cfgPath, explicitFlag string) (*config.Config, error) {
	cfg, err := config.Load(cfgPath)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) && explicitFlag == "" {
		return config.LoadFromBytes([]byte(starterTemplate))
	}
	return nil, err
}

func failf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	return 1
}

func checkRequiredEnvVars(cfg *config.Config) error {
	for name, p := range cfg.Providers {
		if p.DefaultAPIKeyEnv != "" && os.Getenv(p.DefaultAPIKeyEnv) == "" {
			return fmt.Errorf(
				"%s env var required (config provider %q references it)",
				p.DefaultAPIKeyEnv,
				name,
			)
		}
	}
	return nil
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

func newMux(httpHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	healthHandler := func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}
		})
	}
	rootHandler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	mux.Handle("GET /health", healthHandler())
	mux.Handle("HEAD /health", healthHandler())
	mux.Handle("/", rootHandler(httpHandler))
	return mux
}
