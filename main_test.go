package main

import (
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

func TestCheckRequiredEnvVars_PresetEnvVarMissing(t *testing.T) {
	t.Setenv("NIM_API_KEY", "")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "nim", Model: "x", APIKeyEnv: "NIM_API_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing NIM_API_KEY")
	}
	if !strings.Contains(err.Error(), "NIM_API_KEY") || !strings.Contains(err.Error(), "nim") {
		t.Errorf("error should mention env var and provider: %v", err)
	}
}

func TestCheckRequiredEnvVars_PerModelOverrideMissing(t *testing.T) {
	t.Setenv("NIM_API_KEY", "set")
	t.Setenv("OPENAI_API_KEY", "")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "openai", Model: "gpt-4", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing OPENAI_API_KEY")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error should mention env var: %v", err)
	}
}

func TestCheckRequiredEnvVars_AllSet(t *testing.T) {
	t.Setenv("NIM_API_KEY", "k1")
	t.Setenv("OPENAI_API_KEY", "k2")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "nim", Model: "x", APIKeyEnv: "NIM_API_KEY"},
			"b": {Provider: "openai", Model: "gpt-4", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckRequiredEnvVars_CustomNoDefault(t *testing.T) {
	t.Setenv("NIM_API_KEY", "k1")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "custom", Model: "x", BaseURL: "https://x", APIKeyEnv: "CUSTOM_KEY"},
		},
	}
	t.Setenv("CUSTOM_KEY", "k2")
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error for custom (no preset default): %v", err)
	}
}

func TestCheckRequiredEnvVars_ProviderNotReferenced(t *testing.T) {
	t.Setenv("NIM_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "k2")
	cfg := &config.Config{
		Models: map[string]config.Model{
			"a": {Provider: "openai", Model: "gpt-4", BaseURL: "https://x", APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	if err := checkRequiredEnvVars(cfg); err != nil {
		t.Errorf("unexpected error when nim not referenced: %v", err)
	}
}

func TestCheckRequiredEnvVars_MappingMissingEnv(t *testing.T) {
	t.Setenv("NIM_API_KEY", "k1")
	t.Setenv("MY_MAPPING_KEY", "")
	cfg := &config.Config{
		Models: map[string]config.Model{},
		Mappings: map[string]config.Model{
			"opus": {Provider: "openai", Model: "gpt-4", APIKeyEnv: "MY_MAPPING_KEY"},
		},
	}
	err := checkRequiredEnvVars(cfg)
	if err == nil {
		t.Fatal("expected error for missing mapping env")
	}
	if !strings.Contains(err.Error(), "MY_MAPPING_KEY") {
		t.Errorf("error should mention mapping env: %v", err)
	}
}
