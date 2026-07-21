package web

import (
	"testing"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

func TestBuildMappingRows_ProvenanceFields(t *testing.T) {
	t.Setenv("TEST_API_KEY", "present")

	tests := []struct {
		name        string
		mappingName string
		mapping     config.Mapping
		provider    config.Provider
		wantAddedAt string
		wantEnv     bool
		wantFamily  string
	}{
		{
			name:        "all three signals present",
			mappingName: "opus",
			mapping: config.Mapping{
				ProviderName: "go",
				ModelString:  "deepseek-v4-pro",
				AddedAt:      "2026-07-06",
			},
			provider:    config.Provider{Behavior: "mix", DefaultAPIKeyEnv: "TEST_API_KEY"},
			wantAddedAt: "2026-07-06",
			wantEnv:     true,
			wantFamily:  "opus",
		},
		{
			name:        "added_at blank shows empty",
			mappingName: "custom-map",
			mapping: config.Mapping{
				ProviderName: "go",
				ModelString:  "minimax-m3",
			},
			provider:    config.Provider{Behavior: "mix", DefaultAPIKeyEnv: "TEST_API_KEY"},
			wantAddedAt: "",
			wantEnv:     true,
			wantFamily:  "",
		},
		{
			name:        "env var missing",
			mappingName: "haiku",
			mapping: config.Mapping{
				ProviderName: "zen",
				ModelString:  "claude-sonnet-4-6",
				AddedAt:      "2026-07-10",
			},
			provider:    config.Provider{Behavior: "mix", DefaultAPIKeyEnv: "UNSET_ENV_VAR_XYZ"},
			wantAddedAt: "2026-07-10",
			wantEnv:     false,
			wantFamily:  "haiku",
		},
		{
			name:        "keyless provider env stays false",
			mappingName: "customthing",
			mapping: config.Mapping{
				ProviderName: "nim",
				ModelString:  "step-3.5",
				AddedAt:      "2026-07-12",
			},
			provider:    config.Provider{Behavior: "openai"},
			wantAddedAt: "2026-07-12",
			wantEnv:     false,
			wantFamily:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := map[string]config.Provider{tt.mapping.ProviderName: tt.provider}
			cfg := &config.Config{
				Providers: providers,
				Mappings:  map[string]config.Mapping{tt.mappingName: tt.mapping},
			}
			rows := buildMappingRows(cfg, providers, nil, "")
			if len(rows) != 1 {
				t.Fatalf("got %d rows, want 1", len(rows))
			}
			row := rows[0]
			if row.AddedAt != tt.wantAddedAt {
				t.Errorf("AddedAt = %q, want %q", row.AddedAt, tt.wantAddedAt)
			}
			if row.EnvPresent != tt.wantEnv {
				t.Errorf("EnvPresent = %v, want %v", row.EnvPresent, tt.wantEnv)
			}
			if row.Family != tt.wantFamily {
				t.Errorf("Family = %q, want %q", row.Family, tt.wantFamily)
			}
		})
	}
}

func TestBuildMappingRows_ExtractFamilyNoDefault(t *testing.T) {
	// A mapping name with no family keyword must produce an empty Family,
	// not the "default" catch-all.
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
		Mappings: map[string]config.Mapping{
			"xyz": {ProviderName: "nim", ModelString: "step-3.5"},
		},
	}
	rows := buildMappingRows(cfg, cfg.Providers, nil, "")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Family != "" {
		t.Errorf("Family = %q, want empty (no default catch-all leaked)", rows[0].Family)
	}
}

func TestBuildMappingRows_EmptyConfig(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings:  map[string]config.Mapping{},
	}
	rows := buildMappingRows(cfg, cfg.Providers, nil, "")
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestExtractFamily_Exported(t *testing.T) {
	// Sanity: the exported function still behaves as the proxy package expects.
	family, found := proxy.ExtractFamily("opus-4-5")
	if !found || family != "opus" {
		t.Errorf("ExtractFamily(opus-4-5) = %q, %v — want opus, true", family, found)
	}
}
