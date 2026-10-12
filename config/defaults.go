package config

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// mergeProviderDefaults fills the empty declarative fields of p from the
// generated metadata for name and sets the runtime-only flags. Unlike
// applyDefaults it never injects an absent provider into the map — it only
// enriches a value the caller already holds.
//
// The returned OpenAI pointer may be the shared, read-only pointer from
// providerDefaults. That is safe because OpenAIOptions is never mutated after
// load, on either the load path or the write path. If it is ever made mutable
// per-request, this must become a deep copy.
func mergeProviderDefaults(name string, p Provider) Provider {
	defaults, ok := providerDefaults[name]
	if !ok {
		return p
	}
	if p.DefaultBaseURL == "" {
		p.DefaultBaseURL = defaults.DefaultBaseURL
	}
	if p.DefaultAPIKeyEnv == "" {
		p.DefaultAPIKeyEnv = defaults.DefaultAPIKeyEnv
	}
	if p.OpenAI == nil {
		p.OpenAI = defaults.OpenAI
	}
	p.RequireBaseURL = defaults.RequireBaseURL
	// Static flag set at code-generation time. If a user overrides DefaultBaseURL
	// to a /v1/messages-suffixed URL on a mix provider whose generated
	// SupportsCountTokens is false, the dispatcher will use local counting even
	// though MixAdapter would route /v1/messages/count_tokens upstream.
	p.SupportsCountTokens = defaults.SupportsCountTokens
	return p
}

// ReapplyProviderDefaults re-derives the generated metadata for a single named
// provider after a write-path mutation. It is the write-path counterpart to
// applyDefaults: the dashboard form cannot express the openai: options block or
// the runtime-only flags, so they must be restored from providerDefaults or the
// edit silently strips the provider's upstream contract.
//
// It touches only the named provider and never injects absent providers, so it
// cannot pollute a user's config with entries they did not ask for. Callers must
// hold the write lock, since it mutates the Providers map.
func (c *Config) ReapplyProviderDefaults(name string) {
	p, ok := c.Providers[name]
	if !ok {
		return
	}
	c.Providers[name] = mergeProviderDefaults(name, p)
}

// applyDefaults merges generated provider defaults into the user's Providers
// map. It fills empty DefaultBaseURL / DefaultAPIKeyEnv fields and sets the
// runtime-only RequireBaseURL / SupportsCountTokens flags from the generated
// metadata. Providers in providerDefaults that have a DefaultBaseURL but are
// missing from the user's config are injected so they appear in the TUI and
// provider picker.
func (c *Config) applyDefaults() {
	if c.Providers == nil {
		c.Providers = make(map[string]Provider)
	}
	for name, defaults := range providerDefaults {
		p, ok := c.Providers[name]
		if !ok {
			// Provider not in user's config — inject only if it has a
			// default base URL (ready-to-use providers).
			if defaults.DefaultBaseURL != "" {
				c.Providers[name] = defaults
			}
			continue
		}
		c.Providers[name] = mergeProviderDefaults(name, p)
	}
}

func readConfigFile(path string) ([]byte, error) {
	// #nosec G304 -- path is supplied by the operator (flag/config) and not attacker-controlled
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config: config file not found at %s: %w", path, err)
		}
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}
	return data, nil
}

func yamlUnmarshalStrict(path string, data []byte, cfg *Config) error {
	if err := yaml.UnmarshalWithOptions(data, cfg, yaml.Strict()); err != nil {
		return fmt.Errorf("config: %s: %s: %w", path, yaml.FormatError(err, true, false), err)
	}
	return nil
}
