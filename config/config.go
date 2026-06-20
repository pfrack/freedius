// Package config loads, validates, and exposes the freedius YAML configuration
// (provider defaults and model mappings).
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Config is the top-level configuration loaded from a freedius.yaml file.
//
// Providers is the set of named upstream endpoints: each one declares its
// behavior class (openai / anthropic / mix), the base URL to contact, and the
// env var that holds the API key. Mappings is the routing layer: each one
// names a provider and the freetext model string that should be sent to it.
type Config struct {
	Providers map[string]Provider `yaml:"providers"`
	Mappings  map[string]Mapping  `yaml:"mappings,omitempty"`
}

// Provider describes a single upstream LLM endpoint. Its settings are
// independent of any specific mapping: many mappings can share one Provider.
type Provider struct {
	Behavior         string `yaml:"behavior"`
	DefaultBaseURL   string `yaml:"default_base_url,omitempty"`
	DefaultAPIKeyEnv string `yaml:"default_api_key_env,omitempty"`
	AnthropicVersion string `yaml:"anthropic_version,omitempty"`
	// RequireBaseURL and SupportsCountTokens are runtime-only flags populated
	// by applyDefaults from the generated providerDefaults map. They do not
	// round-trip through YAML.
	RequireBaseURL      bool `yaml:"-"`
	SupportsCountTokens bool `yaml:"-"`
}

// Mapping binds a freedius-facing name to an upstream Provider plus the
// freetext model string that should be requested from it.
type Mapping struct {
	ProviderName string `yaml:"provider_name"`
	ModelString  string `yaml:"model_string"`
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

	cfg, err := loadFromUnmarshaled(data)
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(path); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadFromBytes parses, defaults, and validates a freedius configuration from
// raw YAML bytes. It mirrors Load but skips the file-read step so the embedded
// starter template can be used without writing to disk.
func LoadFromBytes(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("config: empty config bytes")
	}
	cfg, err := loadFromUnmarshaled(data)
	if err != nil {
		return nil, err
	}
	if err := cfg.validate("<bytes>"); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadFromUnmarshaled parses data, applies defaults, and asserts that the
// result has at least one provider or mapping. The path argument is used
// purely for error messages.
func loadFromUnmarshaled(data []byte) (*Config, error) {
	var cfg Config
	if err := yamlUnmarshalStrict("<bytes>", data, &cfg); err != nil {
		return nil, err
	}

	if len(cfg.Providers) == 0 && len(cfg.Mappings) == 0 {
		return nil, fmt.Errorf("config: input contains no model mappings")
	}

	cfg.applyDefaults()
	return &cfg, nil
}

// UsesProvider reports whether any provider entry or mapping references the
// given provider name. It checks the providers map directly and walks the
// mappings to find ProviderName references.
func (c *Config) UsesProvider(name string) bool {
	if _, ok := c.Providers[name]; ok {
		return true
	}
	for _, m := range c.Mappings {
		if m.ProviderName == name {
			return true
		}
	}
	return false
}

func (c *Config) validate(path string) error {
	for name, p := range c.Providers {
		if err := validateProvider(path, name, p); err != nil {
			return err
		}
	}
	for name, m := range c.Mappings {
		if err := validateMapping(path, name, m, c.Providers); err != nil {
			return err
		}
	}
	return nil
}

func validateProvider(path, name string, p Provider) error {
	switch p.Behavior {
	case "openai", "anthropic", "mix":
		// valid
	default:
		return fmt.Errorf(
			"config: config file at %s: provider %q has invalid behavior %q (allowed: openai, anthropic, mix)",
			path,
			name,
			p.Behavior,
		)
	}
	if p.DefaultBaseURL != "" {
		u, err := url.Parse(p.DefaultBaseURL)
		if err != nil {
			return fmt.Errorf(
				"config: config file at %s: provider %q has invalid default_base_url %q: %v",
				path,
				name,
				p.DefaultBaseURL,
				err,
			)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf(
				"config: config file at %s: provider %q has default_base_url with invalid scheme %q (allowed: http, https)",
				path,
				name,
				u.Scheme,
			)
		}
	}
	if p.DefaultAPIKeyEnv != "" && strings.ContainsAny(p.DefaultAPIKeyEnv, "\r\n=") {
		return fmt.Errorf(
			"config: config file at %s: provider %q has default_api_key_env with invalid characters (must not contain CR, LF, or =)",
			path,
			name,
		)
	}
	if p.RequireBaseURL && p.DefaultBaseURL == "" {
		return fmt.Errorf(
			"config: config file at %s: provider %q requires default_base_url but none is set",
			path,
			name,
		)
	}
	return nil
}

func validateMapping(path, name string, m Mapping, providers map[string]Provider) error {
	if m.ProviderName == "" {
		return fmt.Errorf(
			"config: config file at %s: mapping %q has no \"provider_name\" field",
			path,
			name,
		)
	}
	if _, ok := providers[m.ProviderName]; !ok {
		return fmt.Errorf(
			"config: config file at %s: mapping %q references unknown provider %q (known: %s)",
			path,
			name,
			m.ProviderName,
			strings.Join(sortedProviderNames(providers), ", "),
		)
	}
	if m.ModelString == "" {
		return fmt.Errorf(
			"config: config file at %s: mapping %q has no \"model_string\" field",
			path,
			name,
		)
	}
	if strings.ContainsAny(m.ModelString, "\r\n:") {
		return fmt.Errorf(
			"config: config file at %s: mapping %q has unsafe \"model_string\" value (must not contain CR, LF, or colon)",
			path,
			name,
		)
	}
	return nil
}

func sortedProviderNames(providers map[string]Provider) []string {
	keys := make([]string, 0, len(providers))
	for k := range providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Marshal serializes the config to YAML bytes. All user-facing fields are
// tagged with yaml struct tags; runtime-only fields (RequireBaseURL,
// SupportsCountTokens) carry yaml:"-" so they are not emitted.
func (c *Config) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
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

	if parent := filepath.Dir(path); parent != "." && parent != "" {
		// #nosec G301 -- user-owned config directory; group/other read keeps tools (e.g. Claude Code) compatible
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("config: create parent dir %s: %w", parent, err)
		}
	}

	// #nosec G306 -- starter config is non-sensitive and should be readable by tooling
	if err := os.WriteFile(path, data, 0o644); err != nil {
		_ = os.Rename(path+".bak", path)
		return fmt.Errorf("config: write %s: %w", path, err)
	}

	return nil
}
