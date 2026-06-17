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

type AnthropicCompatibleAdapter struct {
	logger        *slog.Logger
	verboseErrors bool
}

func NewAnthropicCompatibleAdapter(
	logger *slog.Logger,
	verboseErrors bool,
) *AnthropicCompatibleAdapter {
	return &AnthropicCompatibleAdapter{
		logger:        logger.With("component", "adapter.anthropic"),
		verboseErrors: verboseErrors,
	}
}

func (a *AnthropicCompatibleAdapter) Handle(
	w http.ResponseWriter,
	r *http.Request,
	m config.Model,
	body []byte,
) error {
	if m.BaseURL == "" {
		return fmt.Errorf("%s adapter (anthropic-compat): missing base_url", originalOr(m))
	}
	apiKey := os.Getenv(m.APIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf(
			"%s adapter (anthropic-compat): env var %s is not set",
			originalOr(m),
			m.APIKeyEnv,
		)
	}
	target, err := url.Parse(m.BaseURL)
	if err != nil {
		return fmt.Errorf(
			"%s adapter (anthropic-compat): invalid base_url %q: %w",
			originalOr(m),
			m.BaseURL,
			err,
		)
	}
	apiVersion := m.AnthropicVersion
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
