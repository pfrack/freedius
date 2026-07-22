// Package main implements the freedius binary: a single static executable
// that runs the HTTP proxy and embedded web dashboard.
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
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	_ "embed"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/envinject"
	"github.com/pfrack/freedius/internal/eventstream"
	"github.com/pfrack/freedius/proxy"
	"github.com/pfrack/freedius/proxy/web"
)

const (
	defaultHost               = "127.0.0.1"
	defaultPort               = 8082
	shutdownTimeout           = 5 * time.Second
	readHeaderTimeout         = 5 * time.Second
	readTimeout               = 30 * time.Second
	idleTimeout               = 120 * time.Second
	defaultStreamTimeout      = 5 * time.Minute
	defaultFallbackTimeoutMul = 2
)

var allowedHosts = map[string]struct{}{
	"127.0.0.1": {},
	"0.0.0.0":   {},
}

var version = "dev"

//go:embed templates/starter.yaml
var starterTemplate string

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is the entry point: starts the proxy and web dashboard, then blocks
// until shutdown. --version and --help are handled before flag parsing.
func run(args []string) int {
	if code, done := handleEarlyArgs(args); done {
		return code
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
	flagUIPort := fs.Int("ui-port", 0, "web UI port (overrides FREEDIUS_UI_PORT; default 8083)")
	flagUIHost := fs.String("ui-host", "", "web UI bind address (127.0.0.1 or 0.0.0.0; default 127.0.0.1)")
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

	logFormat := resolveLogFormat(*flagLogFormat)
	logSink := proxy.NewLogSink(1000)
	logger, err := newLogger(logFormat, os.Stderr, logSink)
	if err != nil {
		return failf("freedius: %v", err)
	}
	slog.SetDefault(logger)

	streamTimeout := resolveStreamTimeout(*flagStreamTimeout)

	port := resolveInt(*flagPort, setFlags["port"], "FREEDIUS_PORT", defaultPort)
	if port < 1 || port > 65535 {
		return failf("freedius: invalid --port value: %d (allowed: 1-65535)", port)
	}

	host := defaultHost
	if setFlags["host"] {
		host = *flagHost
	} else if v := os.Getenv("FREEDIUS_HOST"); v != "" {
		host = v
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

	if err := checkRequiredEnvVars(cfg); err != nil {
		return failf("freedius: %s", err)
	}

	serverLogger := logger.With("component", "server")
	serverLogger.Info(
		fmt.Sprintf("freedius listening on http://%s", net.JoinHostPort(host, strconv.Itoa(port))),
		"host", host,
		"port", port,
	)

	if !*flagNoExportHint {
		fmt.Fprintln(os.Stderr, envinject.Snippet(host, port))
	}

	registry := proxy.NewDefaultRegistry(logger, streamTimeout, verboseErrors, nil)
	dispatcher := proxy.NewDispatcher(
		cfg, registry, logger, verboseErrors,
		resolveFallbackTimeoutMultiplier(), streamTimeout,
	)
	bus := proxy.NewEventBus(1000)

	server, serverErr := startProxyServer(host, port, bus, dispatcher, logger, verboseErrors)
	if err := waitForBind(serverErr); err != nil {
		return failf("freedius: %v", err)
	}

	uiPort := resolveUIPort(*flagUIPort, setFlags["ui-port"])
	uiHost := resolveUIHost(*flagUIHost)

	mc := proxy.NewModelsCache()
	h := &eventstream.Handlers{
		Bus:         bus,
		LogSink:     logSink,
		Cfg:         cfg,
		Registry:    registry,
		Host:        uiHost,
		Port:        uiPort,
		StartTime:   time.Now(),
		AuthToken:   os.Getenv("FREEDIUS_UI_TOKEN"),
		CfgPath:     cfgPath,
		ModelsCache: mc,
	}
	webServer := web.NewServer(uiHost, uiPort, h, logger)
	// Bind synchronously so a port conflict on :8083 fails the process
	// (mirrors the proxy server's waitForBind pattern). Dashboard silently
	// disappearing when the port is taken is the failure mode this prevents.
	if err := webServer.Listen(); err != nil {
		return failf("freedius: web server bind: %v", err)
	}
	go func() {
		if err := webServer.Serve(); err != nil {
			logger.Error("web server error", "err", err)
		}
	}()

	logger.Info("web dashboard on http://" + net.JoinHostPort(uiHost, strconv.Itoa(uiPort)))
	return waitForShutdownWithWeb(server, webServer, serverErr, logger)
}

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

func startProxyServer(
	host string,
	port int,
	bus *proxy.EventBus,
	dispatcher *proxy.Dispatcher,
	logger *slog.Logger,
	verboseErrors bool,
) (*http.Server, <-chan error) {
	httpHandler := proxy.RecoverMiddleware(logger, verboseErrors, dispatcher)
	httpHandler = proxy.EventBusMiddleware(bus, httpHandler)
	httpHandler = proxy.AccessLogMiddleware(logger, httpHandler)
	httpHandler = proxy.RequestIDMiddleware(httpHandler)

	server := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           newMux(httpHandler),
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

	return server, serverErr
}

// waitForShutdown blocks until SIGINT or SIGTERM, then gracefully shuts down
// the server and runs any additional cleanup.
func waitForShutdown(server *http.Server, cleanup func() error) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig

	if cleanup != nil {
		if err := cleanup(); err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return server.Shutdown(ctx)
}

func waitForShutdownWithWeb(
	server *http.Server,
	webServer *web.Server,
	serverErr <-chan error,
	logger *slog.Logger,
) int {
	if err := waitForShutdown(server, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return webServer.Shutdown(ctx)
	}); err != nil {
		logger.Error("shutdown error", "err", err)
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
	fs.Int("ui-port", 0, "web UI port (default 8083)")
	fs.String("ui-host", "", "web UI bind address (default 127.0.0.1)")
	fs.PrintDefaults()
}

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

func getVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "(devel)" && v != "" {
			return v
		}
	}
	return "dev"
}

func handleEarlyArgs(args []string) (int, bool) {
	for _, a := range args {
		if a == "--version" {
			fmt.Printf("freedius %s\n", getVersion())
			return 0, true
		}
		if a == "--help" || a == "-h" {
			printUsage(os.Stderr)
			return 0, true
		}
	}
	return 0, false
}

func resolveLogFormat(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("FREEDIUS_LOG"); v != "" {
		return v
	}
	return "text"
}

func resolveStreamTimeout(flagVal time.Duration) time.Duration {
	if flagVal != 0 {
		return flagVal
	}
	if v := os.Getenv("FREEDIUS_STREAM_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultStreamTimeout
}

func resolveFallbackTimeoutMultiplier() int {
	if v := os.Getenv("FREEDIUS_FALLBACK_TIMEOUT_MULTIPLIER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return defaultFallbackTimeoutMul
}

func checkRequiredEnvVars(cfg *config.Config) error {
	providers := cfg.ProvidersSnapshot()
	for name, m := range cfg.MappingsSnapshot() {
		p, ok := providers[m.ProviderName]
		if !ok {
			continue
		}
		if p.DefaultAPIKeyEnv != "" && os.Getenv(p.DefaultAPIKeyEnv) == "" {
			return fmt.Errorf(
				"%s env var required (mapping %q references provider %q)",
				p.DefaultAPIKeyEnv,
				name,
				m.ProviderName,
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

func resolveUIPort(flagVal int, flagSet bool) int {
	if flagSet {
		return flagVal
	}
	if v := os.Getenv("FREEDIUS_UI_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 8083
}

var defaultUIHost = "127.0.0.1"

func resolveUIHost(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("FREEDIUS_UI_HOST"); v != "" {
		return v
	}
	return defaultUIHost
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
