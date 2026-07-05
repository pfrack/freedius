package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pfrack/freedius/config"
)

// ModelView represents a single model from an upstream /v1/models response.
type ModelView struct {
	ID          string
	DisplayName string
}

type modelsEntry struct {
	Models    []ModelView
	FetchedAt time.Time
	Err       string
}

// ModelsCache is a concurrency-safe, in-memory cache for fetched model lists.
// It follows the RWMutex+map pattern from Config, EventBus, and LogSink.
type ModelsCache struct {
	mu      sync.RWMutex
	entries map[string]modelsEntry
}

// NewModelsCache creates a new, empty ModelsCache.
func NewModelsCache() *ModelsCache {
	return &ModelsCache{
		entries: make(map[string]modelsEntry),
	}
}

// Get returns the cached models for a provider. On a cache miss, returns
// (nil, zero, nil). When the entry has an error, returns the models (if any)
// and the error.
func (c *ModelsCache) Get(name string) ([]ModelView, time.Time, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[name]
	if !ok {
		return nil, time.Time{}, nil
	}
	if entry.Err != "" {
		return entry.Models, entry.FetchedAt, fmt.Errorf("%s", entry.Err)
	}
	return entry.Models, entry.FetchedAt, nil
}

// Set stores models (and optionally an error) for a provider, recording the
// current time as FetchedAt.
func (c *ModelsCache) Set(name string, models []ModelView, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := modelsEntry{
		Models:    models,
		FetchedAt: time.Now(),
	}
	if err != nil {
		entry.Err = err.Error()
	}
	c.entries[name] = entry
}

func deriveModelsURL(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	p := parsed.Path

	if strings.HasSuffix(p, "/chat/completions") {
		parsed.Path = p[:len(p)-len("/chat/completions")] + "/models"
		return parsed.String(), nil
	}
	if strings.HasSuffix(p, "/messages") {
		parsed.Path = p[:len(p)-len("/messages")] + "/models"
		return parsed.String(), nil
	}
	parsed.Path = strings.TrimRight(p, "/") + "/models"
	return parsed.String(), nil
}

type modelsResponse struct {
	Data []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"data"`
}

// FetchModels calls the upstream /v1/models endpoint for the given provider
// and parses the response into a []ModelView. Auth headers are set based on
// the provider's Behavior (openai → Bearer, anthropic → x-api-key+version,
// mix → resolved via Protocol or URL sniffing). Returns (nil, nil) when the
// API key env var is set but empty (graceful skip, not an error).
func FetchModels(ctx context.Context, provider config.Provider) ([]ModelView, error) {
	modelsURL, err := deriveModelsURL(provider.DefaultBaseURL)
	if err != nil {
		return nil, fmt.Errorf("derive models URL: %w", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: false,
			Proxy:             http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	behavior := provider.Behavior
	if behavior == "mix" {
		behavior = resolveMixProtocol(provider.DefaultBaseURL, provider.Protocol)
	}

	switch behavior {
	case "anthropic":
		if provider.DefaultAPIKeyEnv != "" {
			apiKey := os.Getenv(provider.DefaultAPIKeyEnv)
			if apiKey == "" {
				return nil, nil
			}
			req.Header.Set("x-api-key", apiKey)
			apiVersion := provider.AnthropicVersion
			if apiVersion == "" {
				apiVersion = "2023-06-01"
			}
			req.Header.Set("anthropic-version", apiVersion)
		}
	case "openai":
		if provider.DefaultAPIKeyEnv != "" {
			apiKey := os.Getenv(provider.DefaultAPIKeyEnv)
			if apiKey == "" {
				return nil, nil
			}
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed modelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse models response: %w", err)
	}

	models := make([]ModelView, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		displayName := m.DisplayName
		if displayName == "" {
			displayName = m.ID
		}
		models = append(models, ModelView{ID: m.ID, DisplayName: displayName})
	}
	return models, nil
}

func resolveMixProtocol(baseURL, protocol string) string {
	if protocol != "" {
		return protocol
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "openai"
	}
	if strings.HasSuffix(parsed.Path, "/v1/messages") {
		return "anthropic"
	}
	return "openai"
}
