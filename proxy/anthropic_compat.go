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
	logger *slog.Logger
}

func NewAnthropicCompatibleAdapter(logger *slog.Logger) *AnthropicCompatibleAdapter {
	return &AnthropicCompatibleAdapter{logger: logger.With("component", "adapter.anthropic")}
}

func (a *AnthropicCompatibleAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	if m.BaseURL == "" {
		return fmt.Errorf("%s adapter (anthropic-compat): missing base_url", originalOr(m))
	}
	apiKey := os.Getenv(m.APIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("%s adapter (anthropic-compat): env var %s is not set", originalOr(m), m.APIKeyEnv)
	}
	target, err := url.Parse(m.BaseURL)
	if err != nil {
		return fmt.Errorf("%s adapter (anthropic-compat): invalid base_url %q: %w", originalOr(m), m.BaseURL, err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Authorization", "Bearer "+apiKey)
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.Header.Set("Authorization", "Bearer "+apiKey)
		},
		ErrorHandler: freediusErrorHandler(a.logger),
	}
	rp.ServeHTTP(w, r)
	return nil
}
