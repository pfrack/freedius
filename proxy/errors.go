package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

func forwardUpstreamError(w http.ResponseWriter, resp *http.Response) error {
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, err := io.Copy(w, resp.Body)
	return err
}

// freediusErrorHandler returns a transport-error handler for httputil.ReverseProxy
// that emits the unified error JSON shape (`error` / `message` / `detail` /
// `request_id`). Client cancellations are still logged at Debug and produce
// no response body — the connection simply closes. The `detail` field is
// gated on verboseErrors, matching writeErrorJSON (proxy.go:165).
func freediusErrorHandler(logger *slog.Logger, verboseErrors bool) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, context.Canceled) {
			logger.Debug("client disconnect", "request_id", RequestIDFromContext(r.Context()), "path", r.URL.Path)
			return
		}
		logger.Error("upstream transport error",
			"request_id", RequestIDFromContext(r.Context()),
			"path", r.URL.Path,
			"err", err,
		)
		body := map[string]string{
			"error":   "upstream_unreachable",
			"message": "upstream not reachable",
		}
		if verboseErrors {
			body["detail"] = err.Error()
		}
		if id := RequestIDFromContext(r.Context()); id != "" {
			body["request_id"] = id
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(body)
	}
}
