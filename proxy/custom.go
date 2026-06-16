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

type CustomAdapter struct {
	logger *slog.Logger
}

func NewCustomAdapter(logger *slog.Logger) *CustomAdapter {
	return &CustomAdapter{logger: logger.With("component", "adapter.custom")}
}

func (a *CustomAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
	if m.BaseURL == "" {
		return fmt.Errorf("custom adapter: missing base_url")
	}
	apiKey := os.Getenv(m.APIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("custom adapter: env var %s is not set", m.APIKeyEnv)
	}
	target, err := url.Parse(m.BaseURL)
	if err != nil {
		return fmt.Errorf("custom adapter: invalid base_url %q: %w", m.BaseURL, err)
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
