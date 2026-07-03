package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/pfrack/freedius/config"
)

// ValidationError holds per-field validation errors. It implements error
// and JSON-marshals as {"error":"validation_failed","message":"...","fields":{...}}.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return "validation failed"
}

// MarshalJSON implements json.Marshaler for ValidationError.
func (e *ValidationError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"error":   "validation_failed",
		"message": "validation failed",
		"fields":  e.Fields,
	})
}

// decodeProviderForm decodes a provider form from the request body.
func decodeProviderForm(r *http.Request) (string, config.Provider, error) {
	if err := r.ParseForm(); err != nil {
		return "", config.Provider{}, fmt.Errorf("parse form: %w", err)
	}
	name := strings.TrimSpace(r.FormValue("name"))
	p := config.Provider{
		Behavior:         strings.TrimSpace(r.FormValue("behavior")),
		DefaultBaseURL:   strings.TrimSpace(r.FormValue("default_base_url")),
		DefaultAPIKeyEnv: strings.TrimSpace(r.FormValue("default_api_key_env")),
		Protocol:         strings.TrimSpace(r.FormValue("protocol")),
	}
	return name, p, nil
}

// decodeMappingForm decodes a mapping form from the request body.
func decodeMappingForm(r *http.Request) (string, config.Mapping, error) {
	if err := r.ParseForm(); err != nil {
		return "", config.Mapping{}, fmt.Errorf("parse form: %w", err)
	}
	name := strings.TrimSpace(r.FormValue("name"))
	m := config.Mapping{
		ProviderName: strings.TrimSpace(r.FormValue("provider_name")),
		ModelString:  strings.TrimSpace(r.FormValue("model_string")),
	}
	return name, m, nil
}

// validateProviderFields validates all provider fields and returns a
// ValidationError with per-field errors, or nil if all valid.
func validateProviderFields(name string, p config.Provider) *ValidationError {
	fields := map[string]string{}

	if err := validateProviderName(name); err != nil {
		fields["name"] = err.Error()
	}
	if err := validateProviderBehavior(p.Behavior); err != nil {
		fields["behavior"] = err.Error()
	}
	if err := validateProviderBaseURL(p.DefaultBaseURL); err != nil {
		fields["default_base_url"] = err.Error()
	}
	if err := validateProviderAPIKeyEnv(p.DefaultAPIKeyEnv); err != nil {
		fields["default_api_key_env"] = err.Error()
	}
	if err := validateProviderProtocol(p.Protocol); err != nil {
		fields["protocol"] = err.Error()
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// validateMappingFields validates all mapping fields and returns a
// ValidationError with per-field errors, or nil if all valid.
func validateMappingFields(name string, m config.Mapping, cfg *config.Config) *ValidationError {
	fields := map[string]string{}

	if err := validateMappingName(name); err != nil {
		fields["name"] = err.Error()
	}
	if err := validateMappingModel(m.ModelString); err != nil {
		fields["model_string"] = err.Error()
	}
	if m.ProviderName == "" {
		fields["provider_name"] = "required"
	} else if !cfg.HasProvider(m.ProviderName) {
		fields["provider_name"] = "provider does not exist"
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func validateProviderName(name string) error {
	if name == "" {
		return fmt.Errorf("required")
	}
	if strings.ContainsAny(name, "\r\n:") {
		return fmt.Errorf("must not contain CR, LF, or colon")
	}
	return nil
}

func validateProviderBehavior(behavior string) error {
	switch behavior {
	case "openai", "anthropic", "mix":
		return nil
	default:
		return fmt.Errorf("must be one of: openai, anthropic, mix")
	}
}

func validateProviderBaseURL(s string) error {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must use http or https scheme")
	}
	return nil
}

func validateProviderAPIKeyEnv(s string) error {
	if s == "" {
		return nil
	}
	if strings.ContainsAny(s, "\r\n=") {
		return fmt.Errorf("must not contain CR, LF, or =")
	}
	return nil
}

func validateProviderProtocol(s string) error {
	if s == "" {
		return nil
	}
	if s != "openai" && s != "anthropic" {
		return fmt.Errorf("must be one of: openai, anthropic")
	}
	return nil
}

func validateMappingName(name string) error {
	if name == "" {
		return fmt.Errorf("required")
	}
	if strings.ContainsAny(name, "\r\n:") {
		return fmt.Errorf("must not contain CR, LF, or colon")
	}
	return nil
}

func validateMappingModel(model string) error {
	if model == "" {
		return fmt.Errorf("required")
	}
	if strings.ContainsAny(model, "\r\n:") {
		return fmt.Errorf("must not contain CR, LF, or colon")
	}
	return nil
}

// pathName extracts the resource name from a URL path by stripping a prefix.
func pathName(r *http.Request, prefix string) (string, error) {
	name := strings.TrimPrefix(r.URL.Path, prefix)
	if name == "" {
		return "", fmt.Errorf("missing name in path")
	}
	return name, nil
}
