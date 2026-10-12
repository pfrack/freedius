package web

import (
	"net/http"
	"net/http/httptest"
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

// --- decodeProviderForm merge semantics ---

func providerFormBody() *http.Request {
	body := "name=zen&behavior=mix&default_base_url=https://opencode.ai/zen/v1/messages" +
		"&default_api_key_env=OPENCODE_API_KEY&protocol=anthropic"
	req := httptest.NewRequest(http.MethodPost, "/v1/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// TestDecodeProviderForm_PreservesOpenAIOptions is the regression guard for the
// data-loss bug: the form has no openai: inputs, so submitting it must not strip
// the options carried on the base value.
func TestDecodeProviderForm_PreservesOpenAIOptions(t *testing.T) {
	base := config.Provider{
		Behavior: "mix",
		OpenAI: &config.OpenAIOptions{
			NoStreamUsage: true,
			PreSendHook:   "sanitizeNIMBody",
			MaxTokensCap:  32768,
		},
	}

	name, p, err := decodeProviderForm(providerFormBody(), base)
	if err != nil {
		t.Fatalf("decodeProviderForm: %v", err)
	}
	if name != "zen" {
		t.Errorf("name = %q, want zen", name)
	}
	if p.OpenAI == nil {
		t.Fatal("OpenAI options block was dropped")
	}
	if !p.OpenAI.NoStreamUsage {
		t.Error("NoStreamUsage lost")
	}
	if p.OpenAI.PreSendHook != "sanitizeNIMBody" {
		t.Errorf("PreSendHook = %q, want sanitizeNIMBody", p.OpenAI.PreSendHook)
	}
	if p.OpenAI.MaxTokensCap != 32768 {
		t.Errorf("MaxTokensCap = %d, want 32768", p.OpenAI.MaxTokensCap)
	}
}

// TestDecodeProviderForm_PreservesAnthropicVersion covers the other field the
// form cannot express.
func TestDecodeProviderForm_PreservesAnthropicVersion(t *testing.T) {
	base := config.Provider{Behavior: "anthropic", AnthropicVersion: "2023-06-01"}

	_, p, err := decodeProviderForm(providerFormBody(), base)
	if err != nil {
		t.Fatalf("decodeProviderForm: %v", err)
	}
	if p.AnthropicVersion != "2023-06-01" {
		t.Errorf("AnthropicVersion = %q, want 2023-06-01", p.AnthropicVersion)
	}
}

// TestDecodeProviderForm_OverlaysSubmittedFields confirms the merge still lets
// the user change what the form does own.
func TestDecodeProviderForm_OverlaysSubmittedFields(t *testing.T) {
	base := config.Provider{
		Behavior:         "anthropic",
		DefaultBaseURL:   "https://old.example/v1/messages",
		DefaultAPIKeyEnv: "OLD_KEY",
		Protocol:         "",
	}

	_, p, err := decodeProviderForm(providerFormBody(), base)
	if err != nil {
		t.Fatalf("decodeProviderForm: %v", err)
	}
	if p.Behavior != "mix" {
		t.Errorf("Behavior = %q, want mix", p.Behavior)
	}
	if p.DefaultBaseURL != "https://opencode.ai/zen/v1/messages" {
		t.Errorf("DefaultBaseURL = %q, want the submitted value", p.DefaultBaseURL)
	}
	if p.DefaultAPIKeyEnv != "OPENCODE_API_KEY" {
		t.Errorf("DefaultAPIKeyEnv = %q, want OPENCODE_API_KEY", p.DefaultAPIKeyEnv)
	}
	if p.Protocol != "anthropic" {
		t.Errorf("Protocol = %q, want anthropic", p.Protocol)
	}
}

// TestDecodeProviderForm_ZeroBaseIsCreateSemantics documents that a zero base
// yields exactly the submitted fields — the create path.
func TestDecodeProviderForm_ZeroBaseIsCreateSemantics(t *testing.T) {
	_, p, err := decodeProviderForm(providerFormBody(), config.Provider{})
	if err != nil {
		t.Fatalf("decodeProviderForm: %v", err)
	}
	if p.OpenAI != nil {
		t.Errorf("create path should not invent OpenAI options, got %+v", p.OpenAI)
	}
	if p.AnthropicVersion != "" {
		t.Errorf("create path should not invent AnthropicVersion, got %q", p.AnthropicVersion)
	}
	if p.Behavior != "mix" || p.Protocol != "anthropic" {
		t.Errorf("submitted fields not decoded: %+v", p)
	}
}
