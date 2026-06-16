package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Models map[string]Model `yaml:"models"`
}

type Model struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	BaseURL   string `yaml:"base_url,omitempty"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

var KnownProviders = map[string]struct{}{
	"nim":    {},
	"zen":    {},
	"go":     {},
	"custom": {},
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config: config file not found at %s: %w", path, err)
		}
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("config: config file at %s contains no model mappings", path)
	}

	var cfg Config
	if err := yaml.UnmarshalWithOptions(data, &cfg, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("config: %s: %s: %w", path, yaml.FormatError(err, true, false), err)
	}

	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf("config: config file at %s contains no model mappings", path)
	}

	for name, m := range cfg.Models {
		if m.Model == "" {
			return nil, fmt.Errorf("config: config file at %s: model %q has no \"model\" field", path, name)
		}
		if m.Provider == "" {
			return nil, fmt.Errorf("config: config file at %s: model %q has no \"provider\" field", path, name)
		}
		if _, ok := KnownProviders[m.Provider]; !ok {
			return nil, fmt.Errorf("config: config file at %s: model %q uses unknown provider %q (known: %s)", path, name, m.Provider, strings.Join(sortedKnownProviders(), ", "))
		}
		if strings.ContainsAny(m.Model, "\r\n:") {
			return nil, fmt.Errorf("config: config file at %s: model %q has unsafe \"model\" value (must not contain CR, LF, or colon)", path, name)
		}
		if err := validateBaseURL(path, name, m); err != nil {
			return nil, err
		}
		if err := validateAPIKeyEnv(path, name, m.APIKeyEnv); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

func validateBaseURL(path, name string, m Model) error {
	if m.BaseURL == "" {
		if m.Provider == "custom" {
			return fmt.Errorf("config: config file at %s: model %q has provider=custom but no base_url", path, name)
		}
		return nil
	}
	parsed, err := url.Parse(m.BaseURL)
	if err != nil {
		return fmt.Errorf("config: config file at %s: model %q has invalid base_url %q: %v", path, name, m.BaseURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("config: config file at %s: model %q has base_url with invalid scheme %q (allowed: http, https)", path, name, parsed.Scheme)
	}
	return nil
}

func validateAPIKeyEnv(path, name, envName string) error {
	if envName == "" {
		return nil
	}
	if strings.ContainsAny(envName, "\r\n=") {
		return fmt.Errorf("config: config file at %s: model %q has unsafe api_key_env %q (must not contain CR, LF, or =)", path, name, envName)
	}
	return nil
}

func sortedKnownProviders() []string {
	keys := make([]string, 0, len(KnownProviders))
	for k := range KnownProviders {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
