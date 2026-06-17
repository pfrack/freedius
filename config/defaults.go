package config

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

type modelDefaults struct {
	BaseURL   string
	APIKeyEnv string
}

var knownProviderDefaults = map[string]modelDefaults{
	"nim": {
		BaseURL:   "https://integrate.api.nvidia.com/v1/chat/completions",
		APIKeyEnv: "NVIDIA_NIM_API_KEY", // #nosec G101 -- env var name, not a credential
	},
	"zen": {
		APIKeyEnv: "OPENCODE_API_KEY", // #nosec G101 -- env var name, not a credential
	},
	"go": {
		APIKeyEnv: "OPENCODE_API_KEY", // #nosec G101 -- env var name, not a credential
	},
	"anthropic": {
		APIKeyEnv: "ANTHROPIC_API_KEY", // #nosec G101 -- env var name, not a credential
	},
}

// ProviderEnvVar returns the conventional environment-variable name that holds
// the API key for the given provider, or "" if the provider has no known default.
func ProviderEnvVar(name string) string {
	d, ok := knownProviderDefaults[name]
	if !ok {
		return ""
	}
	return d.APIKeyEnv
}

func (c *Config) applyDefaults() {
	for name, m := range c.Models {
		c.Models[name] = applyEntryDefaults(m)
	}
	for name, m := range c.Mappings {
		c.Mappings[name] = applyEntryDefaults(m)
	}
}

func applyEntryDefaults(m Model) Model {
	if m.OriginalProvider == "" {
		m.OriginalProvider = m.Provider
	}
	if m.Provider == "custom" {
		m.Provider = "mix"
	}
	d, ok := knownProviderDefaults[m.Provider]
	if !ok {
		return m
	}
	if m.BaseURL == "" {
		m.BaseURL = d.BaseURL
	}
	if m.APIKeyEnv == "" {
		m.APIKeyEnv = d.APIKeyEnv
	}
	if m.Provider == "zen" || m.Provider == "go" {
		m.Provider = "mix"
	}
	return m
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
