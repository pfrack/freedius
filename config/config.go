// Package config loads, validates, and exposes the freedius YAML configuration
// (provider defaults, model mappings, and per-model overrides).
package config

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Config is the top-level configuration loaded from a freedius.yaml file.
type Config struct {
	Models   map[string]Model `yaml:"models"`
	Mappings map[string]Model `yaml:"mappings,omitempty"`
}

// Model describes a single upstream LLM endpoint and its identity inside freedius.
type Model struct {
	Provider         string `yaml:"provider"`
	Model            string `yaml:"model"`
	BaseURL          string `yaml:"base_url,omitempty"`
	APIKeyEnv        string `yaml:"api_key_env,omitempty"`
	AnthropicVersion string `yaml:"anthropic_version,omitempty"`
	Protocol         string `yaml:"protocol,omitempty"`
	OriginalProvider string `yaml:"-"`
}

// Load reads, parses, and validates the freedius configuration at path.
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

// UsesProvider reports whether any model or mapping references the given provider name.
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
		return fmt.Errorf(
			"config: config file at %s: %s %q has no \"model\" field",
			path,
			kind,
			name,
		)
	}
	if m.Provider == "" {
		return fmt.Errorf(
			"config: config file at %s: %s %q has no \"provider\" field",
			path,
			kind,
			name,
		)
	}
	if _, ok := KnownProviders[m.Provider]; !ok {
		return fmt.Errorf(
			"config: config file at %s: %s %q uses unknown provider %q (known: %s)",
			path,
			kind,
			name,
			m.Provider,
			strings.Join(sortedKnownProviders(), ", "),
		)
	}
	if strings.ContainsAny(m.Model, "\r\n:") {
		return fmt.Errorf(
			"config: config file at %s: %s %q has unsafe \"model\" value (must not contain CR, LF, or colon)",
			path,
			kind,
			name,
		)
	}
	if m.BaseURL != "" {
		u, err := url.Parse(m.BaseURL)
		if err != nil {
			return fmt.Errorf(
				"config: config file at %s: %s %q has invalid base_url %q: %v",
				path,
				kind,
				name,
				m.BaseURL,
				err,
			)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf(
				"config: config file at %s: %s %q has base_url with invalid scheme %q (allowed: http, https)",
				path,
				kind,
				name,
				u.Scheme,
			)
		}
	}
	if _, ok := requireBaseURL[m.Provider]; ok && m.BaseURL == "" {
		return fmt.Errorf(
			"config: config file at %s: %s %q has provider=%s but no base_url",
			path,
			kind,
			name,
			m.Provider,
		)
	}
	if m.APIKeyEnv != "" && strings.ContainsAny(m.APIKeyEnv, "\r\n=") {
		return fmt.Errorf(
			"config: config file at %s: %s %q has api_key_env with invalid characters (must not contain CR, LF, or =)",
			path,
			kind,
			name,
		)
	}
	if m.Protocol != "" && m.Protocol != "anthropic" && m.Protocol != "openai" {
		return fmt.Errorf(
			"config: config file at %s: %s %q has invalid protocol %q (allowed: anthropic, openai)",
			path,
			kind,
			name,
			m.Protocol,
		)
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

// Marshal serializes the config to YAML bytes, recovering OriginalProvider
// values for alias entries (zen, go, custom) so they survive the round-trip.
func (c *Config) Marshal() ([]byte, error) {
	clone := &Config{
		Models:   make(map[string]Model, len(c.Models)),
		Mappings: make(map[string]Model, len(c.Mappings)),
	}
	for name, m := range c.Models {
		if m.OriginalProvider != "" && m.OriginalProvider != m.Provider {
			m.Provider = m.OriginalProvider
		}
		m.OriginalProvider = ""
		clone.Models[name] = m
	}
	for name, m := range c.Mappings {
		if m.OriginalProvider != "" && m.OriginalProvider != m.Provider {
			m.Provider = m.OriginalProvider
		}
		m.OriginalProvider = ""
		clone.Mappings[name] = m
	}
	return yaml.Marshal(clone)
}

// Save validates, marshals, and writes the config to path. If path exists,
// it is backed up as path+".bak" before writing. On write failure, the
// backup is restored.
func (c *Config) Save(path string) error {
	if err := c.validate(path); err != nil {
		return err
	}

	data, err := c.Marshal()
	if err != nil {
		return fmt.Errorf("config: marshal %s: %w", path, err)
	}

	if _, statErr := os.Stat(path); statErr == nil {
		if err := os.Rename(path, path+".bak"); err != nil {
			return fmt.Errorf("config: backup %s: %w", path, err)
		}
	}

	// #nosec G306 -- same permissions as freedius init (init.go:70) and starter config
	if err := os.WriteFile(path, data, 0o644); err != nil {
		_ = os.Rename(path+".bak", path)
		return fmt.Errorf("config: write %s: %w", path, err)
	}

	return nil
}
