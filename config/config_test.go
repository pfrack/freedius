package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
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
			name: "valid single provider",
			yaml: `providers:
  nim:
    behavior: openai
    default_api_key_env: NVIDIA_NIM_API_KEY
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Providers) < 1 {
					t.Fatalf("expected at least 1 provider, got %d", len(cfg.Providers))
				}
				p, ok := cfg.Providers["nim"]
				if !ok {
					t.Fatal("expected nim in providers")
				}
				if p.Behavior != "openai" {
					t.Errorf("behavior: got %q, want openai", p.Behavior)
				}
				if p.DefaultBaseURL == "" {
					t.Error("expected nim default_base_url to be filled in")
				}
				if p.DefaultAPIKeyEnv != "NVIDIA_NIM_API_KEY" {
					t.Errorf(
						"default_api_key_env: got %q, want NVIDIA_NIM_API_KEY",
						p.DefaultAPIKeyEnv,
					)
				}
			},
		},
		{
			name: "valid providers and mappings",
			yaml: `providers:
  nim:
    behavior: openai
  custom:
    behavior: mix
    default_base_url: https://example.com/v1/messages
    default_api_key_env: CUSTOM_API_KEY
mappings:
  opus:
    provider_name: nim
    model_string: meta/llama-3.1-70b-instruct
  sonnet:
    provider_name: custom
    model_string: my-sonnet-shim
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Providers) < 2 {
					t.Fatalf("expected at least 2 providers, got %d", len(cfg.Providers))
				}
				if _, ok := cfg.Providers["nim"]; !ok {
					t.Error("missing nim provider")
				}
				if _, ok := cfg.Providers["custom"]; !ok {
					t.Error("missing custom provider")
				}
				if len(cfg.Mappings) != 2 {
					t.Fatalf("expected 2 mappings, got %d", len(cfg.Mappings))
				}
				if _, ok := cfg.Mappings["opus"]; !ok {
					t.Error("missing opus mapping")
				}
				if _, ok := cfg.Mappings["sonnet"]; !ok {
					t.Error("missing sonnet mapping")
				}
				sonnet, ok := cfg.Mappings["sonnet"]
				if !ok {
					t.Fatal("missing sonnet mapping")
				}
				if sonnet.ProviderName != "custom" {
					t.Errorf("sonnet provider_name: got %q, want custom", sonnet.ProviderName)
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
			name:      "empty providers map",
			yaml:      "providers: {}\n",
			wantErr:   true,
			errSubstr: "contains no model mappings",
		},
		{
			name: "malformed YAML",
			yaml: `providers:
  nim:
    behavior: openai
   default_api_key_env: foo
`,
			wantErr:   true,
			errSubstr: "[",
		},
		{
			name: "invalid provider behavior",
			yaml: `providers:
  nim:
    behavior: bogus
    default_api_key_env: NVIDIA_NIM_API_KEY
`,
			wantErr:   true,
			errSubstr: `invalid behavior "bogus"`,
		},
		{
			name: "unknown field typo",
			yaml: `providers:
  nim:
    behav: openai
    default_api_key_env: NVIDIA_NIM_API_KEY
`,
			wantErr:   true,
			errSubstr: `unknown field "behav"`,
		},
		{
			name: "non-string provider behavior",
			yaml: `providers:
  nim:
    behavior: 42
`,
			wantErr: true,
		},
		{
			name: "missing mapping model_string",
			yaml: `providers:
  nim: { behavior: openai }
mappings:
  opus:
    provider_name: nim
`,
			wantErr:   true,
			errSubstr: `mapping "opus" has no "model_string" field`,
		},
		{
			name: "missing mapping provider_name",
			yaml: `providers:
  nim: { behavior: openai }
mappings:
  opus:
    model_string: x
`,
			wantErr:   true,
			errSubstr: `mapping "opus" has no "provider_name" field`,
		},
		{
			name: "mapping references unknown provider",
			yaml: `providers:
  nim: { behavior: openai }
mappings:
  opus:
    provider_name: doesnotexist
    model_string: x
`,
			wantErr:   true,
			errSubstr: `references unknown provider "doesnotexist"`,
		},
		{
			name:      "mapping model_string header-unsafe characters",
			yaml:      "providers:\n  nim: { behavior: openai }\nmappings:\n  opus:\n    provider_name: nim\n    model_string: \"foo\\r\\nX-Injected: bar\"\n",
			wantErr:   true,
			errSubstr: "unsafe \"model_string\" value",
		},
		{
			name: "openai provider without default_base_url",
			yaml: `providers:
  openai:
    behavior: openai
    default_api_key_env: OPENAI_API_KEY
`,
			wantErr:   true,
			errSubstr: `requires default_base_url`,
		},
		{
			name: "anthropic provider without default_base_url",
			yaml: `providers:
  anthropic:
    behavior: anthropic
    default_api_key_env: ANTHROPIC_API_KEY
`,
			wantErr: false,
		},
		{
			name: "default_base_url with invalid scheme",
			yaml: `providers:
  openai:
    behavior: openai
    default_base_url: ftp://example.com/v1/chat/completions
    default_api_key_env: OPENAI_API_KEY
`,
			wantErr:   true,
			errSubstr: `invalid scheme`,
		},
		{
			name:      "default_api_key_env with newline",
			yaml:      "providers:\n  openai:\n    behavior: openai\n    default_base_url: https://example.com\n    default_api_key_env: \"OPENAI\\nKEY\"\n",
			wantErr:   true,
			errSubstr: "default_api_key_env with invalid characters",
		},
		{
			name: "valid openai provider with all fields",
			yaml: `providers:
  openai:
    behavior: openai
    default_base_url: https://api.openai.com/v1/chat/completions
    default_api_key_env: OPENAI_API_KEY
`,
			check: func(t *testing.T, cfg *Config) {
				p, ok := cfg.Providers["openai"]
				if !ok {
					t.Fatal("missing openai provider")
				}
				if p.DefaultBaseURL != "https://api.openai.com/v1/chat/completions" {
					t.Errorf("default_base_url: got %q", p.DefaultBaseURL)
				}
			},
		},
		{
			name: "valid mix provider with anthropic-version",
			yaml: `providers:
  custom:
    behavior: mix
    default_base_url: https://example.com/v1/messages
    default_api_key_env: CUSTOM_KEY
    anthropic_version: 2023-06-01
`,
			check: func(t *testing.T, cfg *Config) {
				p := cfg.Providers["custom"]
				if p.AnthropicVersion != "2023-06-01" {
					t.Errorf("anthropic_version: got %q", p.AnthropicVersion)
				}
			},
		},
		{
			name: "valid mappings only",
			yaml: `providers:
  nim: { behavior: openai }
mappings:
  opus:
    provider_name: nim
    model_string: meta/llama-3.1-70b-instruct
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Mappings) != 1 {
					t.Fatalf("expected 1 mapping, got %d", len(cfg.Mappings))
				}
				if _, ok := cfg.Mappings["opus"]; !ok {
					t.Fatal("missing opus mapping")
				}
			},
		},
		{
			name: "mappings with invalid scheme on provider",
			yaml: `providers:
  openai:
    behavior: openai
    default_base_url: ftp://example.com
mappings:
  opus:
    provider_name: openai
    model_string: gpt-4
`,
			wantErr:   true,
			errSubstr: `invalid scheme`,
		},
		{
			name: "default_api_key_env with equals",
			yaml: `providers:
  openai:
    behavior: openai
    default_base_url: https://x
    default_api_key_env: "KEY=VALUE"
`,
			wantErr:   true,
			errSubstr: "default_api_key_env with invalid characters",
		},
		{
			name: "empty mapping key accepted (validation gap)",
			yaml: `providers:
  nim: { behavior: openai }
mappings:
  "":
    provider_name: nim
    model_string: x
`,
			wantErr: false,
		},
		{
			name: "empty behavior string",
			yaml: `providers:
  nim:
    behavior: ""
`,
			wantErr:   true,
			errSubstr: `invalid behavior`,
		},
		{
			name: "models empty but mappings non-empty",
			yaml: `providers:
  nim: { behavior: openai }
mappings:
  opus:
    provider_name: nim
    model_string: x
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Providers) < 1 {
					t.Errorf("expected at least 1 provider, got %d", len(cfg.Providers))
				}
				if len(cfg.Mappings) != 1 {
					t.Errorf("expected 1 mapping, got %d", len(cfg.Mappings))
				}
			},
		},
		{
			name:      "empty mappings block alone is not enough",
			yaml:      "providers: {}\nmappings: {}\n",
			wantErr:   true,
			errSubstr: "contains no model mappings",
		},
		{
			name: "valid protocol openai",
			yaml: `providers:
  custom:
    behavior: mix
    default_base_url: https://example.com/v1
    default_api_key_env: KEY
    protocol: openai
`,
			check: func(t *testing.T, cfg *Config) {
				p := cfg.Providers["custom"]
				if p.Protocol != "openai" {
					t.Errorf("protocol: got %q, want openai", p.Protocol)
				}
			},
		},
		{
			name: "valid protocol anthropic",
			yaml: `providers:
  custom:
    behavior: mix
    default_base_url: https://example.com/v1
    default_api_key_env: KEY
    protocol: anthropic
`,
			check: func(t *testing.T, cfg *Config) {
				p := cfg.Providers["custom"]
				if p.Protocol != "anthropic" {
					t.Errorf("protocol: got %q, want anthropic", p.Protocol)
				}
			},
		},
		{
			name: "invalid protocol",
			yaml: `providers:
  custom:
    behavior: mix
    default_base_url: https://example.com/v1
    default_api_key_env: KEY
    protocol: grpc
`,
			wantErr:   true,
			errSubstr: `invalid protocol "grpc"`,
		},
		{
			name: "protocol omitted defaults to empty",
			yaml: `providers:
  custom:
    behavior: mix
    default_base_url: https://example.com/v1/messages
    default_api_key_env: KEY
`,
			check: func(t *testing.T, cfg *Config) {
				p := cfg.Providers["custom"]
				if p.Protocol != "" {
					t.Errorf("protocol: got %q, want empty", p.Protocol)
				}
			},
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

func TestLoadFromBytes(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid YAML",
			yaml: `providers:
  nim: { behavior: openai }
mappings:
  opus:
    provider_name: nim
    model_string: meta/llama
`,
		},
		{
			name:    "empty bytes",
			yaml:    "",
			wantErr: true,
		},
		{
			name:    "no mappings or providers",
			yaml:    "providers: {}\n",
			wantErr: true,
		},
		{
			name: "invalid behavior",
			yaml: `providers:
  nim: { behavior: bogus }
`,
			wantErr: true,
		},
		{
			name:    "malformed YAML",
			yaml:    "providers:\n  nim:\n   foo: bar\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadFromBytes([]byte(tt.yaml))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got cfg=%+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, ok := cfg.Providers["nim"]; !ok {
				t.Error("expected nim provider after LoadFromBytes")
			}
		})
	}
}

func TestLoadFromBytes_DoesNotTouchFS(t *testing.T) {
	// LoadFromBytes must not require any path on disk; ensure it works with a
	// deliberately bogus working directory that would fail any os.Stat call.
	t.Setenv("HOME", "/this/path/definitely/does/not/exist")
	cfg, err := LoadFromBytes([]byte("providers:\n  nim: { behavior: openai }\n"))
	if err != nil {
		t.Fatalf("LoadFromBytes should not depend on filesystem, got: %v", err)
	}
	if cfg == nil || cfg.Providers["nim"].Behavior != "openai" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestLoad_FilePathInYAMLError(t *testing.T) {
	// Regression for F4: yamlUnmarshalStrict errors from file-based Load
	// must include the actual file path, not the placeholder "<bytes>".
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  nim:\n   foo: bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected YAML parse error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should mention file path %q, got: %v", path, err)
	}
	if strings.Contains(err.Error(), "<bytes>") {
		t.Errorf("error should NOT mention <bytes>, got: %v", err)
	}
}

func TestProviderDefaults(t *testing.T) {
	expected := []string{
		"nim", "zen", "go", "custom", "openai", "anthropic", "mix",
		"google", "mistral", "deepseek", "groq", "together", "fireworks", "cohere",
		"ollama", "lmstudio",
	}
	if len(providerDefaults) != len(expected) {
		t.Errorf("providerDefaults has %d entries, want %d", len(providerDefaults), len(expected))
	}
	for _, e := range expected {
		if _, ok := providerDefaults[e]; !ok {
			t.Errorf("providerDefaults missing %q", e)
		}
	}
}

func TestProviderDefaults_SupportsCountTokens(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"nim", false},
		{"openai", false},
		{"anthropic", true},
		{"mix", false},
		{"zen", false},
		{"go", false},
		{"custom", false},
		{"google", false},
		{"mistral", false},
		{"deepseek", false},
		{"groq", false},
		{"together", false},
		{"fireworks", false},
		{"cohere", false},
		{"ollama", false},
		{"lmstudio", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := providerDefaults[tt.name]
			if !ok {
				t.Fatalf("providerDefaults missing %q", tt.name)
			}
			if p.SupportsCountTokens != tt.want {
				t.Errorf("SupportsCountTokens = %v, want %v", p.SupportsCountTokens, tt.want)
			}
		})
	}
}

func TestUsesProvider(t *testing.T) {
	cfg := &Config{
		Providers: map[string]Provider{
			"nim":    {Behavior: "openai"},
			"openai": {Behavior: "openai"},
		},
		Mappings: map[string]Mapping{
			"opus": {ProviderName: "anthropic"},
		},
	}
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"nim in providers", "nim", true},
		{"openai in providers", "openai", true},
		{"anthropic referenced by mapping", "anthropic", true},
		{"zen not used", "zen", false},
		{"go not used", "go", false},
		{"custom not used", "custom", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.UsesProvider(tt.in); got != tt.want {
				t.Errorf("UsesProvider(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestConfig_MarshalBasic(t *testing.T) {
	cfg := &Config{
		Providers: map[string]Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]Mapping{
			"opus": {ProviderName: "nim", ModelString: "meta/llama-3.1-70b-instruct"},
		},
	}
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	p, ok := parsed.Providers["nim"]
	if !ok {
		t.Fatal("missing nim provider after round-trip")
	}
	if p.Behavior != "openai" {
		t.Errorf("behavior = %q, want openai", p.Behavior)
	}
	m, ok := parsed.Mappings["opus"]
	if !ok {
		t.Fatal("missing opus mapping after round-trip")
	}
	if m.ModelString != "meta/llama-3.1-70b-instruct" {
		t.Errorf("model_string = %q, want meta/llama-3.1-70b-instruct", m.ModelString)
	}
}

func TestConfig_MarshalOmitsRuntimeFields(t *testing.T) {
	cfg := &Config{
		Providers: map[string]Provider{
			"nim": {
				Behavior:            "openai",
				RequireBaseURL:      true,
				SupportsCountTokens: false,
			},
		},
	}

	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	yamlStr := string(data)
	if strings.Contains(yamlStr, "require_base_url") {
		t.Errorf("runtime-only require_base_url must not appear in YAML, got:\n%s", yamlStr)
	}
	if strings.Contains(yamlStr, "supports_count_tokens") {
		t.Errorf("runtime-only supports_count_tokens must not appear in YAML, got:\n%s", yamlStr)
	}
}

func TestConfig_MarshalOmitEmpty(t *testing.T) {
	cfg := &Config{
		Providers: map[string]Provider{
			"test": {
				Behavior: "openai",
			},
		},
	}

	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	yamlStr := string(data)
	if strings.Contains(yamlStr, "default_base_url") {
		t.Errorf("expected no default_base_url in output (empty, omitempty), got:\n%s", yamlStr)
	}
	if strings.Contains(yamlStr, "default_api_key_env") {
		t.Errorf("expected no default_api_key_env in output (empty, omitempty), got:\n%s", yamlStr)
	}
	if strings.Contains(yamlStr, "protocol") {
		t.Errorf("expected no protocol in output (empty, omitempty), got:\n%s", yamlStr)
	}
}

func TestConfig_SaveBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freedius.yaml")

	initial := `providers:
  nim: { behavior: openai }
mappings:
  opus:
    provider_name: nim
    model_string: meta/llama-3.1-70b-instruct
`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cfg.Mappings["opus"] = Mapping{ProviderName: "nim", ModelString: "meta/llama-4"}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify backup file exists and contains original content.
	bakData, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !strings.Contains(string(bakData), "meta/llama-3.1-70b-instruct") {
		t.Errorf("backup should contain original mapping, got:\n%s", string(bakData))
	}

	// Verify saved file contains new content.
	savedData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	if !strings.Contains(string(savedData), "meta/llama-4") {
		t.Errorf("saved file should contain new mapping, got:\n%s", string(savedData))
	}
}

func TestConfig_SaveCreatesParentDir(t *testing.T) {
	// Save must MkdirAll the parent dir so lazy startup works without a
	// pre-existing ~/.config/freedius. This replaces the old
	// TestConfig_SaveRollbackOnWriteFailure behavior.
	dir := t.TempDir()
	path := filepath.Join(dir, "deeply", "nested", "config", "freedius.yaml")

	cfg, err := LoadFromBytes([]byte("providers:\n  nim: { behavior: openai }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save should create parent dirs, got: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist after Save: %v", err)
	}
}

func TestConfig_RoundTripLoadMarshalLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freedius.yaml")

	input := `providers:
  nim:
    behavior: openai
  custom:
    behavior: mix
    default_base_url: https://example.com/v1/messages
    default_api_key_env: CUSTOM_API_KEY
  openai:
    behavior: openai
    default_base_url: https://api.openai.com/v1/chat/completions
    default_api_key_env: OPENAI_API_KEY
mappings:
  opus:
    provider_name: nim
    model_string: meta/llama-3.1-70b-instruct
  deepseek:
    provider_name: custom
    model_string: deepseek-v4-pro
  gpt:
    provider_name: openai
    model_string: gpt-4
`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}

	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	roundTripPath := filepath.Join(dir, "roundtrip.yaml")
	if err := os.WriteFile(roundTripPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg2, err := Load(roundTripPath)
	if err != nil {
		t.Fatalf("second Load: %v\nmarshaled YAML:\n%s", err, string(data))
	}

	// Verify key properties survived the round-trip.
	if len(cfg2.Providers) != len(cfg.Providers) {
		t.Errorf("provider count: %d, want %d", len(cfg2.Providers), len(cfg.Providers))
	}
	if len(cfg2.Mappings) != len(cfg.Mappings) {
		t.Errorf("mapping count: %d, want %d", len(cfg2.Mappings), len(cfg.Mappings))
	}

	// Verify mappings preserved provider_name and model_string.
	for name, want := range cfg.Mappings {
		got, ok := cfg2.Mappings[name]
		if !ok {
			t.Errorf("mapping %q lost in round-trip", name)
			continue
		}
		if got.ProviderName != want.ProviderName {
			t.Errorf(
				"mapping %q provider_name: got %q, want %q",
				name,
				got.ProviderName,
				want.ProviderName,
			)
		}
		if got.ModelString != want.ModelString {
			t.Errorf(
				"mapping %q model_string: got %q, want %q",
				name,
				got.ModelString,
				want.ModelString,
			)
		}
	}
}

func TestConfig_ThemeRoundTrip(t *testing.T) {
	input := `providers:
  test:
    behavior: openai
theme: zenburn
`
	cfg, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	if cfg.Theme != "zenburn" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "zenburn")
	}

	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	cfg2, err := LoadFromBytes(data)
	if err != nil {
		t.Fatalf("round-trip LoadFromBytes: %v\nYAML:\n%s", err, string(data))
	}
	if cfg2.Theme != "zenburn" {
		t.Errorf("round-trip Theme = %q, want %q", cfg2.Theme, "zenburn")
	}
}

func TestConfig_ThemeOmitEmpty(t *testing.T) {
	input := `providers:
  test:
    behavior: openai
`
	cfg, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	if cfg.Theme != "" {
		t.Errorf("Theme = %q, want empty", cfg.Theme)
	}

	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if containsTheme := strings.Contains(string(data), "theme:"); containsTheme {
		t.Errorf("marshaled YAML contains theme: field (should be omitted)\n%s", string(data))
	}
}
