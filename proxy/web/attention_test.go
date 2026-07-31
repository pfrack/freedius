package web

import (
	"testing"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

func TestComputeAlerts_NoIssues(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"haiku": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	providers := cfg.ProvidersSnapshot()
	mappingStats := map[string]proxy.MappingStats{}
	providerStats := map[string]proxy.ProviderStats{}

	alerts := computeAlerts(cfg, mappingStats, providerStats, providers)
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d: %+v", len(alerts), alerts)
	}
}

func TestComputeAlerts_MissingAPIKey(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NONEXISTENT_KEY_FOR_TEST_ABC"},
		},
		Mappings: map[string]config.Mapping{
			"haiku": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	providers := cfg.ProvidersSnapshot()
	mappingStats := map[string]proxy.MappingStats{}
	providerStats := map[string]proxy.ProviderStats{}

	alerts := computeAlerts(cfg, mappingStats, providerStats, providers)
	if len(alerts) == 0 {
		t.Fatal("expected at least 1 alert for missing API key")
	}
	found := false
	for _, a := range alerts {
		if a.Severity == "error" && contains(a.Message, "missing API key") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'missing API key' error alert, got: %+v", alerts)
	}
}

func TestComputeAlerts_HighErrorRate(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"haiku": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	providers := cfg.ProvidersSnapshot()
	mappingStats := map[string]proxy.MappingStats{}
	providerStats := map[string]proxy.ProviderStats{
		"nim": {
			RequestCount:    10,
			ErrorCount:      8,
			RecentErrorRate: 0.8,
			LastSuccess:     time.Now().Add(-10 * time.Minute),
			LastError:       time.Now().Add(-1 * time.Minute),
		},
	}

	alerts := computeAlerts(cfg, mappingStats, providerStats, providers)
	found := false
	for _, a := range alerts {
		if a.Severity == "warning" && contains(a.Message, "error rate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error rate warning alert, got: %+v", alerts)
	}
}

func TestComputeAlerts_MissingProvider(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Mappings: map[string]config.Mapping{
			"haiku": {ProviderName: "nonexistent", ModelString: "m1"},
		},
	}
	providers := cfg.ProvidersSnapshot()
	mappingStats := map[string]proxy.MappingStats{}
	providerStats := map[string]proxy.ProviderStats{}

	alerts := computeAlerts(cfg, mappingStats, providerStats, providers)
	found := false
	for _, a := range alerts {
		if a.Severity == "error" && contains(a.Message, "missing provider") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'missing provider' error alert, got: %+v", alerts)
	}
}

func TestComputeAlerts_SortedErrorsFirst(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai", DefaultAPIKeyEnv: "NONEXISTENT_KEY_XYZ"},
		},
		Mappings: map[string]config.Mapping{
			"haiku": {ProviderName: "nim", ModelString: "m1"},
		},
	}
	providers := cfg.ProvidersSnapshot()
	mappingStats := map[string]proxy.MappingStats{}
	providerStats := map[string]proxy.ProviderStats{
		"nim": {
			RequestCount:    10,
			ErrorCount:      8,
			RecentErrorRate: 0.8,
		},
	}

	alerts := computeAlerts(cfg, mappingStats, providerStats, providers)
	if len(alerts) < 2 {
		t.Fatalf("expected at least 2 alerts, got %d", len(alerts))
	}
	// First alert should be severity "error" (missing key), second "warning".
	if alerts[0].Severity != "error" {
		t.Errorf("expected first alert to be error, got %q", alerts[0].Severity)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
