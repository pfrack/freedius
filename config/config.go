package config

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type Config struct {
	Models   map[string]Model `yaml:"models"`
	Mappings map[string]Model `yaml:"mappings,omitempty"`
}

type Model struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	BaseURL   string `yaml:"base_url,omitempty"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

var KnownProviders = map[string]struct{}{
	"nim":      {},
	"zen":      {},
	"go":       {},
	"custom":   {},
	"openai":   {},
	"anthropic": {},
}

func Load(path string) (*Config, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("config: config file at %s contains no model mappings", path)
	}

	var cfg Config
	if err := yamlUnmarshalStrict(path, data, &cfg); err != nil {
		return nil, err
	}

	if len(cfg.Models) == 0 && len(cfg.Mappings) == 0 {
		return nil, fmt.Errorf("config: config file at %s contains no model mappings", path)
	}

	cfg.applyDefaults()

	if err := cfg.validate(path); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) UsesProvider(name string) bool {
	for _, m := range c.Models {
		if m.Provider == name {
			return true
		}
	}
	for _, m := range c.Mappings {
		if m.Provider == name {
			return true
		}
	}
	return false
}

func (c *Config) validate(path string) error {
	for name, m := range c.Models {
		if err := validateModel(path, "model", name, m); err != nil {
			return err
		}
	}
	for name, m := range c.Mappings {
		if err := validateModel(path, "mapping", name, m); err != nil {
			return err
		}
	}
	return nil
}

func validateModel(path, kind, name string, m Model) error {
	if m.Model == "" {
		return fmt.Errorf("config: config file at %s: %s %q has no \"model\" field", path, kind, name)
	}
	if m.Provider == "" {
		return fmt.Errorf("config: config file at %s: %s %q has no \"provider\" field", path, kind, name)
	}
	if _, ok := KnownProviders[m.Provider]; !ok {
		return fmt.Errorf("config: config file at %s: %s %q uses unknown provider %q (known: %s)", path, kind, name, m.Provider, strings.Join(sortedKnownProviders(), ", "))
	}
	if strings.ContainsAny(m.Model, "\r\n:") {
		return fmt.Errorf("config: config file at %s: %s %q has unsafe \"model\" value (must not contain CR, LF, or colon)", path, kind, name)
	}
	if m.BaseURL != "" {
		u, err := url.Parse(m.BaseURL)
		if err != nil {
			return fmt.Errorf("config: config file at %s: %s %q has invalid base_url %q: %v", path, kind, name, m.BaseURL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("config: config file at %s: %s %q has base_url with invalid scheme %q (allowed: http, https)", path, kind, name, u.Scheme)
		}
	}
	if (m.Provider == "openai" || m.Provider == "anthropic") && m.BaseURL == "" {
		return fmt.Errorf("config: config file at %s: %s %q has provider=%s but no base_url", path, kind, name, m.Provider)
	}
	if m.APIKeyEnv != "" && strings.ContainsAny(m.APIKeyEnv, "\r\n=") {
		return fmt.Errorf("config: config file at %s: %s %q has api_key_env with invalid characters (must not contain CR, LF, or =)", path, kind, name)
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
