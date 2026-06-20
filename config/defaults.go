package config

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// applyDefaults merges generated provider defaults into the user's Providers
// map. It fills empty DefaultBaseURL / DefaultAPIKeyEnv fields and sets the
// runtime-only RequireBaseURL / SupportsCountTokens flags from the generated
// metadata.
func (c *Config) applyDefaults() {
	if c.Providers == nil {
		return
	}
	for name, defaults := range providerDefaults {
		p, ok := c.Providers[name]
		if !ok {
			continue
		}
		if p.DefaultBaseURL == "" {
			p.DefaultBaseURL = defaults.DefaultBaseURL
		}
		if p.DefaultAPIKeyEnv == "" {
			p.DefaultAPIKeyEnv = defaults.DefaultAPIKeyEnv
		}
		if p.AnthropicVersion == "" {
			p.AnthropicVersion = defaults.AnthropicVersion
		}
		p.RequireBaseURL = defaults.RequireBaseURL
		p.SupportsCountTokens = defaults.SupportsCountTokens
		c.Providers[name] = p
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
