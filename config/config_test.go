package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantErr   bool
		errSubstr string
		check     func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid single model",
			yaml: `models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Models) != 1 {
					t.Fatalf("expected 1 model, got %d", len(cfg.Models))
				}
				m, ok := cfg.Models["claude-opus-4"]
				if !ok {
					t.Fatal("expected claude-opus-4 in models")
				}
				if m.Provider != "nim" {
					t.Errorf("provider: got %q, want nim", m.Provider)
				}
				if m.Model != "meta/llama-3.1-70b-instruct" {
					t.Errorf("model: got %q, want meta/llama-3.1-70b-instruct", m.Model)
				}
			},
		},
		{
			name: "valid two models",
			yaml: `models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Models) != 2 {
					t.Fatalf("expected 2 models, got %d", len(cfg.Models))
				}
				if _, ok := cfg.Models["claude-opus-4"]; !ok {
					t.Error("missing claude-opus-4")
				}
				if _, ok := cfg.Models["claude-sonnet-4"]; !ok {
					t.Error("missing claude-sonnet-4")
				}
			},
		},
		{
			name:      "empty file",
			yaml:      ``,
			wantErr:   true,
			errSubstr: "contains no model mappings",
		},
		{
			name:      "empty models map",
			yaml:      "models: {}\n",
			wantErr:   true,
			errSubstr: "contains no model mappings",
		},
		{
			name: "malformed YAML",
			yaml: `models:
  claude-opus-4:
    provider: nim
   model: foo
`,
			wantErr:   true,
			errSubstr: "[",
		},
		{
			name: "unknown provider",
			yaml: `models:
  claude-opus-4:
    provider: foo
    model: bar
`,
			wantErr:   true,
			errSubstr: `model "claude-opus-4" uses unknown provider "foo" (known: custom, go, nim, zen)`,
		},
		{
			name: "unknown field typo",
			yaml: `models:
  claude-opus-4:
    provder: nim
    model: foo
`,
			wantErr:   true,
			errSubstr: `unknown field "provder"`,
		},
		{
			name: "missing model field",
			yaml: `models:
  claude-opus-4:
    provider: nim
`,
			wantErr:   true,
			errSubstr: `model "claude-opus-4" has no "model" field`,
		},
		{
			name: "missing provider field",
			yaml: `models:
  claude-opus-4:
    model: foo
`,
			wantErr:   true,
			errSubstr: `model "claude-opus-4" has no "provider" field`,
		},
		{
			name: "non-string provider",
			yaml: `models:
  claude-opus-4:
    provider: 42
    model: foo
`,
			wantErr: true,
		},
		{
			name: "model with header-unsafe characters",
			yaml: "models:\n  claude-opus-4:\n    provider: nim\n    model: \"foo\\r\\nX-Injected: bar\"\n",
			wantErr:   true,
			errSubstr: "unsafe \"model\" value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "freedius.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; cfg=%+v", cfg)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.yaml")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error does not wrap os.ErrNotExist: %v", err)
	}
	if !strings.Contains(err.Error(), "config file not found at") {
		t.Errorf("error does not contain expected message: %v", err)
	}
}

func TestKnownProviders(t *testing.T) {
	expected := []string{"nim", "zen", "go", "custom"}
	if len(KnownProviders) != len(expected) {
		t.Errorf("KnownProviders has %d entries, want %d", len(KnownProviders), len(expected))
	}
	for _, e := range expected {
		if _, ok := KnownProviders[e]; !ok {
			t.Errorf("KnownProviders missing %q", e)
		}
	}
}
