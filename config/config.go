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
	"sync"

	"github.com/goccy/go-yaml"
)

// Config is the top-level configuration loaded from a freedius.yaml file.
//
// Providers is the set of named upstream endpoints: each one declares its
// behavior class (openai / anthropic / mix), the base URL to contact, and the
// env var that holds the API key. Mappings is the routing layer: each one
// names a provider and the freetext model string that should be sent to it.
//
// Concurrent access is guarded by an internal RWMutex: the dispatcher holds
// a read lock while resolving a request, while the TUI holds a write lock
// when mutating the maps (submitForm, delete). Snapshot helpers below return
// copies so renderers can iterate without holding the lock.
type Config struct {
	mu        sync.RWMutex
	Providers map[string]Provider `yaml:"providers"`
	Mappings  map[string]Mapping  `yaml:"mappings,omitempty"`
	Theme     string              `yaml:"theme,omitempty"`
}

// Provider describes a single upstream LLM endpoint. Its settings are
// independent of any specific mapping: many mappings can share one Provider.
type Provider struct {
	Behavior         string `yaml:"behavior"`
	DefaultBaseURL   string `yaml:"default_base_url,omitempty"`
	DefaultAPIKeyEnv string `yaml:"default_api_key_env,omitempty"`
	AnthropicVersion string `yaml:"anthropic_version,omitempty"`
	// Protocol forces the wire protocol for mix providers ("openai" or
	// "anthropic"). When set, the MixAdapter routes to the matching
	// sub-adapter and normalizes the base URL (appending /v1/messages or
	// /v1/chat/completions if the path doesn't already end with the
	// expected suffix). When empty, the adapter falls back to URL path
	// sniffing. Ignored for non-mix providers.
	Protocol string `yaml:"protocol,omitempty"`
	// RequireBaseURL and SupportsCountTokens are runtime-only flags populated
	// by applyDefaults from the generated providerDefaults map. They do not
	// round-trip through YAML.
	RequireBaseURL      bool `yaml:"-"`
	SupportsCountTokens bool `yaml:"-"`
}

// Mapping binds a freedius-facing name to an upstream Provider plus the
// freetext model string that should be requested from it.
//
// Fallback holds an ordered list of alternate {ProviderName, ModelString}
// targets to try when the primary target fails (pre-flight config error,
// transport failure, or upstream HTTP 4xx/5xx before any response bytes
// reach the client). Each entry reuses the Mapping struct — the Fallback
// field on fallback entries is always nil (no recursive chaining).
type Mapping struct {
	ProviderName string    `yaml:"provider_name"`
	ModelString  string    `yaml:"model_string"`
	Fallback     []Mapping `yaml:"fallback,omitempty"`
	AddedAt      string    `yaml:"added_at,omitempty"`
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

	cfg, err := loadFromUnmarshaled(path, data)
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
	cfg, err := loadFromUnmarshaled("<bytes>", data)
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
func loadFromUnmarshaled(path string, data []byte) (*Config, error) {
	var cfg Config
	if err := yamlUnmarshalStrict(path, data, &cfg); err != nil {
		return nil, err
	}

	if len(cfg.Providers) == 0 && len(cfg.Mappings) == 0 {
		return nil, fmt.Errorf("config: input contains no model mappings")
	}

	cfg.applyDefaults()
	return &cfg, nil
}

// Lock acquires the writer mutex for the underlying Providers and Mappings
// maps. It must be paired with a matching Unlock. The TUI uses this when
// mutating the maps (submitForm, handleDeleteConfirmKeyPress). Dispatcher
// request handlers should use RLock/RUnlock instead.
func (c *Config) Lock() { c.mu.Lock() }

// Unlock releases the writer mutex.
func (c *Config) Unlock() { c.mu.Unlock() }

// RLock acquires the reader mutex for safe concurrent map reads.
func (c *Config) RLock() { c.mu.RLock() }

// RUnlock releases the reader mutex.
func (c *Config) RUnlock() { c.mu.RUnlock() }

// ProvidersSnapshot returns a copy of the providers map safe for the caller
// to iterate without holding the lock. Useful for rendering loops.
func (c *Config) ProvidersSnapshot() map[string]Provider {
	c.RLock()
	defer c.RUnlock()
	out := make(map[string]Provider, len(c.Providers))
	for k, v := range c.Providers {
		out[k] = v
	}
	return out
}

// MappingsSnapshot returns a copy of the mappings map safe for the caller
// to iterate without holding the lock.
func (c *Config) MappingsSnapshot() map[string]Mapping {
	c.RLock()
	defer c.RUnlock()
	out := make(map[string]Mapping, len(c.Mappings))
	for k, v := range c.Mappings {
		out[k] = v
	}
	return out
}

// HasProvider reports whether the named provider exists. Safe for concurrent
// callers; uses a read lock internally.
func (c *Config) HasProvider(name string) bool {
	c.RLock()
	defer c.RUnlock()
	_, ok := c.Providers[name]
	return ok
}

// UsesProvider reports whether any provider entry or mapping references the
// given provider name. It checks the providers map directly and walks the
// mappings to find ProviderName references. Safe for concurrent callers.
func (c *Config) UsesProvider(name string) bool {
	c.RLock()
	defer c.RUnlock()
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
	if p.Protocol != "" && p.Protocol != "openai" && p.Protocol != "anthropic" {
		return fmt.Errorf(
			"config: config file at %s: provider %q has invalid protocol %q (allowed: openai, anthropic)",
			path,
			name,
			p.Protocol,
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

	// Dedup: the primary counts as the first entry.
	type pair struct{ ProviderName, ModelString string }
	seen := map[pair]bool{
		{m.ProviderName, m.ModelString}: true,
	}
	for i, fb := range m.Fallback {
		if fb.ProviderName == "" {
			return fmt.Errorf(
				"config: config file at %s: mapping %q fallback[%d] has no \"provider_name\" field",
				path,
				name,
				i,
			)
		}
		if _, ok := providers[fb.ProviderName]; !ok {
			return fmt.Errorf(
				"config: config file at %s: mapping %q fallback[%d] references unknown provider %q (known: %s)",
				path,
				name,
				i,
				fb.ProviderName,
				strings.Join(sortedProviderNames(providers), ", "),
			)
		}
		if fb.ModelString == "" {
			return fmt.Errorf(
				"config: config file at %s: mapping %q fallback[%d] has no \"model_string\" field",
				path,
				name,
				i,
			)
		}
		if strings.ContainsAny(fb.ModelString, "\r\n:") {
			return fmt.Errorf(
				"config: config file at %s: mapping %q fallback[%d] has unsafe \"model_string\" value (must not contain CR, LF, or colon)",
				path,
				name,
				i,
			)
		}
		p := pair{fb.ProviderName, fb.ModelString}
		if seen[p] {
			return fmt.Errorf(
				"config: config file at %s: mapping %q has duplicate fallback entry %q/%q",
				path,
				name,
				fb.ProviderName,
				fb.ModelString,
			)
		}
		seen[p] = true
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

// Save validates, marshals, and writes the config to path atomically.
//
// The write is performed by writing to a sibling temp file (path+".tmp") and
// then renaming it over path. The rename is atomic on POSIX file systems, so
// a concurrent reader sees either the old or new content — never a half-
// written file. If path exists before the write, it is first copied to
// path+".bak" so the user can manually recover if the new content is bad.
//
// On write failure, the rename to path is not attempted, so the original
// (and its .bak copy) remain untouched. If the temp-file write itself fails
// midway, the .tmp file may be left behind; the next Save attempt overwrites
// it.
func (c *Config) Save(path string) error {
	if err := c.validate(path); err != nil {
		return err
	}

	data, err := c.Marshal()
	if err != nil {
		return fmt.Errorf("config: marshal %s: %w", path, err)
	}

	return c.SaveData(path, data)
}

// SaveData writes the provided marshalled data to path using the same
// atomic-write pattern as Save (backup, temp file, rename). Unlike Save,
// SaveData does not call validate or Marshal — the caller is responsible
// for providing valid, complete YAML data.
func (c *Config) SaveData(path string, data []byte) error {
	existed := false
	if _, statErr := os.Stat(path); statErr == nil {
		existed = true
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

	tmpPath := path + ".tmp"
	// #nosec G306 -- starter config is non-sensitive and should be readable by tooling
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		// Best-effort cleanup of half-written temp file.
		_ = os.Remove(tmpPath)
		// If the original existed and was backed up, restore from .bak.
		if existed {
			if rerr := os.Rename(path+".bak", path); rerr != nil {
				return fmt.Errorf("config: write %s failed (%v); backup restore from %s also failed: %w",
					path, err, path+".bak", rerr)
			}
		}
		return fmt.Errorf("config: write %s: %w", path, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		if existed {
			if rerr := os.Rename(path+".bak", path); rerr != nil {
				return fmt.Errorf("config: rename %s -> %s failed (%v); backup restore also failed: %w",
					tmpPath, path, err, rerr)
			}
		}
		return fmt.Errorf("config: rename %s -> %s: %w", tmpPath, path, err)
	}

	return nil
}
