// Package proxy implements the freedius HTTP reverse proxy: provider adapters,
// middleware (request ID, recover, access log), and the request dispatcher.
package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/pfrack/freedius/config"
)

// AnthropicCompatibleAdapter forwards requests to an Anthropic-API-compatible
// upstream using an httputil.ReverseProxy (no streaming translation needed).
type AnthropicCompatibleAdapter struct {
	logger        *slog.Logger
	verboseErrors bool
}

// NewAnthropicCompatibleAdapter returns an adapter tagged with the
// "adapter.anthropic" slog component and the given verboseErrors setting.
func NewAnthropicCompatibleAdapter(
	logger *slog.Logger,
	verboseErrors bool,
) *AnthropicCompatibleAdapter {
	return &AnthropicCompatibleAdapter{
		logger:        logger.With("component", "adapter.anthropic"),
		verboseErrors: verboseErrors,
	}
}

// Handle rewrites the request for the upstream Anthropic-API-compatible base
// URL, sets x-api-key / anthropic-version, and serves via ReverseProxy.
func (a *AnthropicCompatibleAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	provider config.Provider,
	mapping config.Mapping,
	body []byte,
) error {
	if provider.DefaultBaseURL == "" {
		return &configError{
			err:     fmt.Errorf("%s adapter (anthropic-compat): missing base_url", mapping.ProviderName),
			errType: "invalid_request_error",
		}
	}
	apiKey := os.Getenv(provider.DefaultAPIKeyEnv)
	if apiKey == "" {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (anthropic-compat): env var %s is not set",
				mapping.ProviderName,
				provider.DefaultAPIKeyEnv,
			),
			errType: "authentication_error",
		}
	}
	target, err := url.Parse(provider.DefaultBaseURL)
	if err != nil {
		return &configError{
			err: fmt.Errorf(
				"%s adapter (anthropic-compat): invalid base_url %q: %w",
				mapping.ProviderName,
				provider.DefaultBaseURL,
				err,
			),
			errType: "invalid_request_error",
		}
	}
	apiVersion := provider.AnthropicVersion
	if apiVersion == "" {
		apiVersion = "2023-06-01"
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("x-api-key", apiKey)
	r.Header.Set("anthropic-version", apiVersion)
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.Header.Set("x-api-key", apiKey)
			pr.Out.Header.Set("anthropic-version", apiVersion)
			pr.Out.Header.Del("Authorization")
		},
		ErrorHandler: freediusErrorHandler(a.logger, a.verboseErrors),
	}
	rp.ServeHTTP(w, r)
	return nil
}
