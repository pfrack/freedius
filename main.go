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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/envinject"
	"github.com/pfrack/freedius/proxy"
)

const (
	defaultHost         = "127.0.0.1"
	defaultPort         = 8082
	shutdownTimeout     = 5 * time.Second
	readHeaderTimeout   = 5 * time.Second
	readTimeout         = 30 * time.Second
	idleTimeout         = 120 * time.Second
	defaultStreamTimeout = 5 * time.Minute
)

var allowedHosts = map[string]struct{}{
	"127.0.0.1": {},
	"0.0.0.0":   {},
}

var version = "dev"

// newLogger constructs the process-wide logger. When format is "json", the
// handler emits structured JSON lines; otherwise it emits the human-readable
// text format.
func newLogger(format string, w io.Writer) (logger *slog.Logger, err error) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("invalid log format %q (allowed: text, json)", format)
	}
}

func main() {
	os.Exit(dispatch(os.Args))
}

func dispatch(argv []string) int {
	sub := "serve"
	args := argv[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "serve":
		return runServe(args)
	case "init":
		return runInit(args)
	case "version":
		fmt.Printf("freedius %s\n", version)
		return 0
	case "help", "-h", "--help":
		printTopLevelHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "freedius: unknown subcommand %q\nRun 'freedius help' for usage.\n", sub)
		return 2
	}
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	flagConfig := fs.String("config", "", "path to config file (auto-resolved if empty)")
	flagPort := fs.Int("port", 0, "port to listen on (overrides FREEDIUS_PORT; default 8080)")
	flagHost := fs.String("host", "", "host to bind (127.0.0.1 or 0.0.0.0; default 127.0.0.1)")
	flagVerboseErrors := fs.Bool("verbose-errors", false, "include upstream error detail in error responses (or set FREEDIUS_VERBOSE_ERRORS=1)")
	flagLogFormat := fs.String("log-format", "", "log output format: text, json (overrides FREEDIUS_LOG; default text)")
	flagStreamTimeout := fs.Duration("stream-timeout", 0, "per-request upstream timeout (overrides FREEDIUS_STREAM_TIMEOUT; default 5m)")
	flagNoExportHint := fs.Bool("no-export-hint", false, "suppress the env-export hint on startup")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: freedius serve [flags]\n\nFlags:\n")
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

	verboseErrors := *flagVerboseErrors || os.Getenv("FREEDIUS_VERBOSE_ERRORS") == "1"

	logFormat := *flagLogFormat
	if logFormat == "" {
		logFormat = os.Getenv("FREEDIUS_LOG")
	}
	if logFormat == "" {
		logFormat = "text"
	}
	baseLogger, err := newLogger(logFormat, os.Stderr)
	if err != nil {
		return failf("freedius: %v", err)
	}
	slog.SetDefault(baseLogger)
	logger := baseLogger

	logger.Info("freedius starting")

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
		baseLogger.Error("config path resolution failed", "err", err)
		return 1
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && *flagConfig == "" {
			baseLogger.Info("no config found, writing default config", "path", cfgPath)
			if parent := filepath.Dir(cfgPath); parent != "." {
				if err := os.MkdirAll(parent, 0o755); err != nil {
					return failf("freedius: cannot create config directory %s: %v", parent, err)
				}
			}
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

	if err := checkRequiredEnvVars(cfg); err != nil {
		return failf("freedius: %s", err)
	}

	serverLogger := logger.With("component", "server")
	serverLogger.Info(fmt.Sprintf("freedius listening on http://%s", net.JoinHostPort(host, strconv.Itoa(port))), "host", host, "port", port)

	if !*flagNoExportHint {
		fmt.Fprintln(os.Stderr, envinject.Snippet(host, port))
	}


	registry := proxy.NewRegistry(map[string]proxy.Provider{
		"nim":       proxy.NewNIMAdapter(logger),
		"custom":    proxy.NewCustomAdapter(logger),
		"openai":    proxy.NewOpenAICompatibleAdapterWithTimeout(logger, streamTimeout),
		"anthropic": proxy.NewAnthropicCompatibleAdapter(logger),
		"mix":       proxy.NewMixAdapter(logger),
	})
	dispatcher := proxy.NewDispatcher(cfg, registry, logger, verboseErrors)
	mux := http.NewServeMux()

	httpHandler := proxy.RecoverMiddleware(logger, verboseErrors, dispatcher)
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

func printTopLevelHelp() {
	fmt.Print(`freedius — local Claude Code proxy

Usage: freedius [<subcommand>] [<flags>]

  serve     Start the proxy server (default)
  init      Generate a starter config file
  version   Print the binary version
  help      Show this help

Run 'freedius <subcommand> --help' for subcommand-specific flags.
`)
}

func failf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	return 1
}

func checkRequiredEnvVars(cfg *config.Config) error {
	for name, m := range cfg.Models {
		if m.APIKeyEnv != "" && os.Getenv(m.APIKeyEnv) == "" {
			return fmt.Errorf("%s env var required (config model %q references it; provider=%s)", m.APIKeyEnv, name, originalProviderName(m))
		}
	}
	for name, m := range cfg.Mappings {
		if m.APIKeyEnv != "" && os.Getenv(m.APIKeyEnv) == "" {
			return fmt.Errorf("%s env var required (config mapping %q references it; provider=%s)", m.APIKeyEnv, name, originalProviderName(m))
		}
	}
	return nil
}

func originalProviderName(m config.Model) string {
	if m.OriginalProvider != "" {
		return m.OriginalProvider
	}
	return m.Provider
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
