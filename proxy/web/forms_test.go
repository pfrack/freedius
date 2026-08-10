package web

import (
	"strings"
	"testing"

	"github.com/pfrack/freedius/config"
)

func TestValidateProviderFields_HappyPath(t *testing.T) {
	p := config.Provider{Behavior: "openai", DefaultBaseURL: "https://api.openai.com"}
	if ve := validateProviderFields("openai", p); ve != nil {
		t.Errorf("unexpected errors: %v", ve.Fields)
	}
}

func TestValidateProviderFields_EmptyName(t *testing.T) {
	p := config.Provider{Behavior: "openai"}
	ve := validateProviderFields("", p)
	if ve == nil {
		t.Fatal("expected validation error for empty name")
	}
	if ve.Fields["name"] != "required" {
		t.Errorf("name error = %q, want required", ve.Fields["name"])
	}
}

func TestValidateProviderFields_NameWithColon(t *testing.T) {
	p := config.Provider{Behavior: "openai"}
	ve := validateProviderFields("bad:name", p)
	if ve == nil {
		t.Fatal("expected validation error for name with colon")
	}
}

func TestValidateProviderFields_InvalidBehavior(t *testing.T) {
	p := config.Provider{Behavior: "invalid"}
	ve := validateProviderFields("test", p)
	if ve == nil {
		t.Fatal("expected validation error for invalid behavior")
	}
	if !strings.Contains(ve.Fields["behavior"], "openai") {
		t.Errorf("behavior error should mention allowed values: %v", ve.Fields["behavior"])
	}
}

func TestValidateProviderFields_InvalidBaseURL(t *testing.T) {
	p := config.Provider{Behavior: "openai", DefaultBaseURL: "ftp://bad"}
	ve := validateProviderFields("test", p)
	if ve == nil {
		t.Fatal("expected validation error for invalid base URL")
	}
}

func TestValidateProviderFields_InvalidAPIKeyEnv(t *testing.T) {
	p := config.Provider{Behavior: "openai", DefaultAPIKeyEnv: "BAD=KEY"}
	ve := validateProviderFields("test", p)
	if ve == nil {
		t.Fatal("expected validation error for API key env with =")
	}
}

func TestValidateProviderFields_InvalidProtocol(t *testing.T) {
	p := config.Provider{Behavior: "mix", Protocol: "grpc"}
	ve := validateProviderFields("test", p)
	if ve == nil {
		t.Fatal("expected validation error for invalid protocol")
	}
}

func TestValidateProviderFields_EmptyOptionalFields(t *testing.T) {
	p := config.Provider{Behavior: "openai"}
	if ve := validateProviderFields("test", p); ve != nil {
		t.Errorf("empty optional fields should be valid: %v", ve.Fields)
	}
}

func TestValidateMappingFields_HappyPath(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"nim": {Behavior: "openai"}},
	}
	m := config.Mapping{ProviderName: "nim", ModelString: "gpt-4"}
	if ve := validateMappingFields("test", m, cfg); ve != nil {
		t.Errorf("unexpected errors: %v", ve.Fields)
	}
}

func TestValidateMappingFields_EmptyName(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{"nim": {}}}
	m := config.Mapping{ProviderName: "nim", ModelString: "gpt-4"}
	ve := validateMappingFields("", m, cfg)
	if ve == nil {
		t.Fatal("expected validation error for empty name")
	}
}

func TestValidateMappingFields_EmptyProvider(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{"nim": {}}}
	m := config.Mapping{ProviderName: "", ModelString: "gpt-4"}
	ve := validateMappingFields("test", m, cfg)
	if ve == nil {
		t.Fatal("expected validation error for empty provider")
	}
}

func TestValidateMappingFields_NonExistentProvider(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{"nim": {}}}
	m := config.Mapping{ProviderName: "nonexistent", ModelString: "gpt-4"}
	ve := validateMappingFields("test", m, cfg)
	if ve == nil {
		t.Fatal("expected validation error for non-existent provider")
	}
	if !strings.Contains(ve.Fields["provider_name"], "does not exist") {
		t.Errorf("provider_name error = %q", ve.Fields["provider_name"])
	}
}

func TestValidateMappingFields_EmptyModel(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{"nim": {}}}
	m := config.Mapping{ProviderName: "nim", ModelString: ""}
	ve := validateMappingFields("test", m, cfg)
	if ve == nil {
		t.Fatal("expected validation error for empty model")
	}
}

func TestValidateMappingFields_ModelWithColonAllowed(t *testing.T) {
	// Colons are legal in HTTP header values and common in vendor model IDs
	// (e.g. "hy3:free", "llama3:8b"), so they must not be rejected.
	cfg := &config.Config{Providers: map[string]config.Provider{"nim": {}}}
	m := config.Mapping{ProviderName: "nim", ModelString: "hy3:free"}
	if ve := validateMappingFields("test", m, cfg); ve != nil {
		t.Fatalf("colon in model_string should be allowed, got %v", ve)
	}
}

func TestValidateMappingFields_ModelWithCRLFRejected(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{"nim": {}}}
	for _, bad := range []string{"foo\r\nX-Injected: bar", "foo\rbar", "foo\nbar"} {
		m := config.Mapping{ProviderName: "nim", ModelString: bad}
		if ve := validateMappingFields("test", m, cfg); ve == nil {
			t.Errorf("expected validation error for model %q", bad)
		}
	}
}

func TestValidationError_JSON(t *testing.T) {
	ve := &ValidationError{Fields: map[string]string{"name": "required"}}
	out, err := ve.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "validation_failed") {
		t.Error("JSON should contain validation_failed")
	}
	if !strings.Contains(s, "required") {
		t.Error("JSON should contain field error")
	}
}
