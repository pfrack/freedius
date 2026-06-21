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
	if code, done := handleEarlyArgs(args); done {
		return code
	}

	// Subcommand dispatch (stop, status, attach) — after help scan, before flag parsing.
	if len(args) > 0 {
		switch args[0] {
		case "stop":
			return handleStop()
		case "status":
			return handleStatus()
		case "attach":
			return handleAttach(args[1:])
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
	flagFg := fs.Bool("fg", false, "run headless in foreground (no TUI, for Docker/scripts)")
	flagDaemon := fs.Bool("daemon", false, "run as background daemon (no TUI, forks to background)")
	flagDaemonShort := fs.Bool("d", false, "shorthand for --daemon")
	fs.Usage = func() { printUsage(os.Stderr) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	daemon := *flagDaemon || *flagDaemonShort
	if daemon && *flagFg {
		return failf("freedius: --daemon and --fg are mutually exclusive")
	}

	// Handle --daemon by forking to background with --fg.
	if daemon {
		return handleDaemonStart()
	}

	verboseErrors := *flagVerboseErrors || os.Getenv("FREEDIUS_VERBOSE_ERRORS") == "1"

	logFormat := resolveLogFormat(*flagLogFormat)
	logSink := proxy.NewLogSink(1000)
	logWriter := io.Discard
	if *flagFg {
		logWriter = os.Stderr
	}
	logger, err := newLogger(logFormat, logWriter, logSink)
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

	if *flagFg {
		return startHeadlessWithIPC(server, bus, logSink, cfg, registry, host, port, logger)
	}

	model := tui.NewDashboard(
		bus.Subscribe(),
		logSink.Subscribe(),
		cfg, registry, dispatcher, cfgPath, host, port, verboseErrors,
		cfg.Theme,
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

// runHeadless runs the proxy in foreground without the TUI. Blocks until a
// shutdown signal is received, then gracefully shuts down.
func runHeadless(server *http.Server, logger *slog.Logger, cleanup func() error) int {
	if err := waitForShutdown(server, cleanup); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	logger.Info("shutdown complete")
	return 0
}

// startHeadlessWithIPC starts the IPC server alongside the HTTP proxy and
// blocks until shutdown. The IPC server's Shutdown is wired as the cleanup
// arg to waitForShutdown so the socket file is removed on graceful exit.
func startHeadlessWithIPC(
	server *http.Server,
	bus *proxy.EventBus,
	logSink *proxy.LogSink,
	cfg *config.Config,
	registry *proxy.Registry,
	host string,
	port int,
	logger *slog.Logger,
) int {
	ipcServer := NewIPCServer(socketPath(), bus, logSink, cfg, registry, host, port)
	go func() {
		if err := ipcServer.ListenAndServe(); err != nil {
			logger.Error("IPC server error", "err", err)
		}
	}()
	return runHeadless(server, logger, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return ipcServer.Shutdown(ctx)
	})
}

func handleStop() int {
	if err := stopDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "freedius: daemon stopped")
	return 0
}

func handleStatus() int {
	running, pid, err := daemonStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "freedius: %v\n", err)
		return 1
	}
	if running {
		fmt.Fprintf(os.Stderr, "freedius: running (PID %d)\n", pid)
		return 0
	}
	fmt.Fprintln(os.Stderr, "freedius: not running")
	return 0
}

func handleDaemonStart() int {
	if err := startDaemon(os.Args[1:]); err != nil {
		return failf("%v", err)
	}
	return 0
}

func printUsage(w io.Writer) {
	usage := `freedius — local Claude Code proxy

Usage: freedius [flags]
       freedius stop
       freedius status

Subcommands:
  stop      Send SIGTERM to a running daemon
  status    Check if a daemon is running
  attach    Connect TUI to a running daemon

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
	fs.Bool("fg", false, "run headless in foreground (no TUI, for Docker/scripts)")
	fs.Bool("daemon", false, "run as background daemon (no TUI, forks to background)")
	fs.Bool("d", false, "shorthand for --daemon")
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

// handleEarlyArgs checks for --version and --help before flag parsing.
// Returns (code, true) if the arg was handled and run() should exit.
func handleEarlyArgs(args []string) (int, bool) {
	for _, a := range args {
		if a == "--version" {
			fmt.Printf("freedius %s\n", version)
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
