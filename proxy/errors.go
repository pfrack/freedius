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
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, err := io.Copy(w, resp.Body)
	return err
}

func writeUpstreamUnreachable(w http.ResponseWriter, logger *slog.Logger, path string, err error) {
	if errors.Is(err, context.Canceled) {
		logger.Debug("client disconnect", "path", path)
		return
	}
	logger.Error("upstream error", "err", err, "path", path)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "upstream_unreachable",
		"detail": err.Error(),
	})
}

func freediusErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		writeUpstreamUnreachable(w, logger, r.URL.Path, err)
	}
}
