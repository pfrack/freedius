package main

import (
	"bytes"
	"encoding/json"
	"io"
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

func TestNewLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, err := newLogger("json", &buf)
	if err != nil {
		t.Fatalf("newLogger(json): %v", err)
	}
	logger.Info("hello", "key", "value")
	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v (raw: %s)", err, out)
	}
	if parsed["msg"] != "hello" {
		t.Errorf("msg: got %v, want hello", parsed["msg"])
	}
	if parsed["key"] != "value" {
		t.Errorf("key: got %v, want value", parsed["key"])
	}
}

func TestNewLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, err := newLogger("text", &buf)
	if err != nil {
		t.Fatalf("newLogger(text): %v", err)
	}
	logger.Info("hello", "key", "value")
	out := buf.String()
	if !strings.Contains(out, "msg=hello") || !strings.Contains(out, "key=value") {
		t.Errorf("text format output missing key= / msg= pairs: %s", out)
	}
	// Ensure output is NOT JSON (text handler produces key=value pairs).
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err == nil {
		t.Errorf("text format should not produce valid JSON, got: %s", out)
	}
}

func TestNewLogger_InvalidFormat(t *testing.T) {
	_, err := newLogger("yaml", io.Discard)
	if err == nil {
		t.Fatal("expected error for invalid log format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should mention invalid format: %v", err)
	}
}
