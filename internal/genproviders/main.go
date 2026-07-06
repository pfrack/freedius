// Command genproviders emits Go source files for the config and proxy
// packages from providers.yaml. It is the implementation of the
// "single source of truth" provider metadata model — adding a provider
// is one YAML entry + `go generate ./...`.
//
// Usage:
//
//	go run ./internal/genproviders -spec providers.yaml -pkg config
//	go run ./internal/genproviders -spec providers.yaml -pkg proxy
//
// The generator emits one file per package:
//
//	config/providers_gen.go  — providerDefaults map (config.Provider values)
//	proxy/adapters_gen.go    — thin wrapper adapters + NewDefaultRegistry
//
// Output is go/format-clean and ready to commit. CI runs the generator
// and asserts no diff to catch stale generated files.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/template"

	"github.com/goccy/go-yaml"
)

// Spec is the top-level structure of providers.yaml.
type Spec struct {
	Providers map[string]Provider `yaml:"providers"`
}

// Provider declares one user-facing provider name and all of its metadata.
//
// The behavior field selects the runtime adapter class (openai / anthropic /
// mix). DefaultBaseURL and DefaultAPIKeyEnv are filled in when the user does
// not set them on the provider. RequireBaseURL marks the provider as needing
// a base_url (false when default_base_url is provided).
type Provider struct {
	Behavior         string         `yaml:"behavior"`
	DefaultBaseURL   string         `yaml:"default_base_url,omitempty"`
	DefaultAPIKeyEnv string         `yaml:"default_api_key_env,omitempty"`
	RequireBaseURL   bool           `yaml:"require_base_url"`
	Manual           bool           `yaml:"manual,omitempty"`
	OpenAI           *OpenAIOptions `yaml:"openai,omitempty"`
}

// OpenAIOptions are openai-behavior-specific adapter tweaks. They are only
// meaningful for providers with behavior: openai.
type OpenAIOptions struct {
	NoStreamUsage bool   `yaml:"no_stream_usage,omitempty"`
	PreSendHook   string `yaml:"pre_send_hook,omitempty"`
}

func (p Provider) needsThinWrapper() bool {
	if p.Manual {
		return false
	}
	if p.Behavior != "openai" {
		return false
	}
	if p.OpenAI == nil {
		return false
	}
	return p.OpenAI.NoStreamUsage || p.OpenAI.PreSendHook != ""
}

// supportsCountTokens derives the runtime SupportsCountTokens flag from the
// spec. The dispatcher consults this at /v1/messages/count_tokens to decide
// between a local BPE estimate and a pass-through to the upstream.
//
// Rules:
//   - anthropic behavior always supports count_tokens
//   - mix behavior supports it iff the default_base_url path ends with /v1/messages
//   - everything else does not
func supportsCountTokens(p Provider) bool {
	if p.Behavior == "anthropic" {
		return true
	}
	if p.Behavior != "mix" {
		return false
	}
	if p.DefaultBaseURL == "" {
		return false
	}
	u, err := url.Parse(p.DefaultBaseURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(u.Path, "/v1/messages")
}

// --- Template data shapes ---

type configTmplData struct {
	ProviderDefaults []providerDefaultEntry
}

type providerDefaultEntry struct {
	Name                string
	Behavior            string
	DefaultBaseURL      string
	DefaultAPIKeyEnv    string
	AnthropicVersion    string
	RequireBaseURL      bool
	SupportsCountTokens bool
}

type proxyTmplData struct {
	Adapters        []adapterEntry
	RegistryEntries []registryEntry
}

type adapterEntry struct {
	Name          string
	TypeName      string
	NoStreamUsage bool
	PreSendHook   string
}

type registryEntry struct {
	Name     string
	CtorCall string
}

// --- Generation entrypoints ---

// GenerateConfig returns the Go source for config/providers_gen.go derived
// from spec. The output is go/format-clean.
func GenerateConfig(spec Spec) ([]byte, error) {
	names := sortedProviderNames(spec)
	var data configTmplData

	for _, name := range names {
		p := spec.Providers[name]
		data.ProviderDefaults = append(data.ProviderDefaults, providerDefaultEntry{
			Name:                name,
			Behavior:            p.Behavior,
			DefaultBaseURL:      p.DefaultBaseURL,
			DefaultAPIKeyEnv:    p.DefaultAPIKeyEnv,
			RequireBaseURL:      p.RequireBaseURL,
			SupportsCountTokens: supportsCountTokens(p),
		})
	}

	return executeTemplate("config", configTemplate, data)
}

// GenerateProxy returns the Go source for proxy/adapters_gen.go derived from
// spec. The output is go/format-clean.
func GenerateProxy(spec Spec) ([]byte, error) {
	var data proxyTmplData

	for name, p := range spec.Providers {
		if !p.needsThinWrapper() {
			continue
		}
		data.Adapters = append(data.Adapters, adapterEntry{
			Name:          name,
			TypeName:      providerTypeName(name),
			NoStreamUsage: p.OpenAI.NoStreamUsage,
			PreSendHook:   p.OpenAI.PreSendHook,
		})
	}

	sort.Slice(data.Adapters, func(i, j int) bool {
		return data.Adapters[i].Name < data.Adapters[j].Name
	})

	// Registry entries wire the runtime adapters by behavior class. Under
	// the providers/mappings schema there are no alias rewrites at load
	// time; all 4 behaviors are wired unconditionally.
	data.RegistryEntries = []registryEntry{
		{Name: "nim", CtorCall: "NewNIMAdapter(logger, streamTimeout)"},
		{Name: "openai", CtorCall: "NewOpenAICompatibleAdapterWithTimeout(logger, streamTimeout)"},
		{Name: "anthropic", CtorCall: "NewAnthropicCompatibleAdapterWithTimeout(logger, verboseErrors, streamTimeout)"},
		{Name: "mix", CtorCall: "NewMixAdapter(logger, verboseErrors, streamTimeout)"},
	}

	return executeTemplate("proxy", proxyTemplate, data)
}

func executeTemplate(name, tmpl string, data any) ([]byte, error) {
	t, err := template.New(name).Delims("<%", "%>").Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("parse %s template: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute %s template: %w", name, err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("go/format %s: %w\n--- raw output ---\n%s", name, err, buf.String())
	}
	return formatted, nil
}

func sortedProviderNames(spec Spec) []string {
	names := make([]string, 0, len(spec.Providers))
	for n := range spec.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func providerTypeName(name string) string {
	if name == "nim" {
		return "NIMAdapter"
	}
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:] + "Adapter"
}

func addBuildTag(tag string, src []byte) []byte {
	tagLine := "//go:build " + tag
	return append([]byte(tagLine+"\n\n"), src...)
}

// --- main ---

func main() {
	var (
		specPath = flag.String("spec", "providers.yaml", "path to providers.yaml")
		pkg      = flag.String("pkg", "", "package to generate for (config|proxy)")
		out      = flag.String("out", "", "output path (default: <pkg>_gen.go in cwd)")
		write    = flag.Bool("write", true, "write output to disk (false: print to stdout)")
		buildTag = flag.String("build-tag", "", "optional //go:build tag to add (phase 1 only)")
	)
	flag.Parse()
	if *pkg == "" {
		log.Fatal("-pkg is required (config|proxy)")
	}

	spec, err := loadSpec(*specPath)
	if err != nil {
		log.Fatalf("load spec: %v", err)
	}

	var output []byte
	switch *pkg {
	case "config":
		output, err = GenerateConfig(*spec)
	case "proxy":
		output, err = GenerateProxy(*spec)
	default:
		log.Fatalf("unknown -pkg: %s", *pkg)
	}
	if err != nil {
		log.Fatalf("generate: %v", err)
	}

	if *buildTag != "" {
		output = addBuildTag(*buildTag, output)
	}

	if !*write {
		if _, err := os.Stdout.Write(output); err != nil {
			log.Fatalf("write stdout: %v", err)
		}
		return
	}

	outPath := *out
	if outPath == "" {
		if *pkg == "config" {
			outPath = "providers_gen.go"
		} else {
			outPath = "adapters_gen.go"
		}
	}
	if err := os.WriteFile(outPath, output, 0o600); err != nil {
		log.Fatalf("write %s: %v", outPath, err)
	}
	fmt.Fprintf(os.Stderr, "genproviders: wrote %s (%d bytes)\n", outPath, len(output))
}

// loadSpec is exported for tests so they can build a Spec programmatically.
func loadSpec(path string) (*Spec, error) {
	// #nosec G304 -- path is supplied by the operator (flag) and not attacker-controlled
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &spec, nil
}

// --- Templates ---

const configTemplate = `// Code generated by genproviders from providers.yaml. DO NOT EDIT.

package config

// providerDefaults is the metadata table for known providers, indexed by
// user-facing provider name. It is merged into Config.Providers by
// applyDefaults to fill in missing fields and to set the runtime-only
// RequireBaseURL and SupportsCountTokens flags.
//
// Every provider is a first-class entry: there is no alias rewriting at
// load time.
var providerDefaults = map[string]Provider{
<% range .ProviderDefaults -%>
	"<% .Name %>": {
		Behavior:            "<% .Behavior %>",
<% if .DefaultBaseURL -%>
		DefaultBaseURL:      "<% .DefaultBaseURL %>", // #nosec G101 -- URL, not a credential
<% end -%>
<% if .DefaultAPIKeyEnv -%>
		DefaultAPIKeyEnv:    "<% .DefaultAPIKeyEnv %>", // #nosec G101 -- env var name, not a credential
<% end -%>
		RequireBaseURL:      <% .RequireBaseURL %>,
		SupportsCountTokens: <% .SupportsCountTokens %>,
	},
<% end -%>
}
`

const proxyTemplate = `// Code generated by genproviders from providers.yaml. DO NOT EDIT.

package proxy

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy/translate"
)

<% range .Adapters -%>
// <% .TypeName %> wraps OpenAICompatibleAdapter with provider-specific
// options (no_stream_usage and pre_send_hook).
type <% .TypeName %> struct {
	inner *OpenAICompatibleAdapter
}

// New<% .TypeName %> returns a "<% .Name %>" provider adapter.
func New<% .TypeName %>(logger *slog.Logger, streamTimeout time.Duration) *<% .TypeName %> {
	inner := NewOpenAICompatibleAdapterWithTimeout(logger, streamTimeout)
	inner.translateOpts = translate.Opts{NoStreamUsage: <% .NoStreamUsage %>}
<% if .PreSendHook -%>
	inner.preSendHook = <% .PreSendHook %>
<% end -%>
	return &<% .TypeName %>{inner: inner}
}

// Handle delegates to the embedded OpenAICompatibleAdapter.
func (a *<% .TypeName %>) Handle(
	w http.ResponseWriter,
	r *http.Request,
	provider config.Provider,
	mapping config.Mapping,
	body []byte,
) error {
	return a.inner.Handle(w, r, provider, mapping, body)
}

<% end -%>
// NewDefaultRegistry returns a Registry wired with the default adapter set.
// streamTimeout and verboseErrors configure the underlying core adapters
// (OpenAICompatible, AnthropicCompatible, Mix). overrides lets callers
// swap any of the default adapters — used for "manual: true" providers
// that need hand-written logic.
func NewDefaultRegistry(
	logger *slog.Logger,
	streamTimeout time.Duration,
	verboseErrors bool,
	overrides map[string]Provider,
) *Registry {
	providers := map[string]Provider{
<% range .RegistryEntries -%>
		"<% .Name %>": <% .CtorCall %>,
<% end -%>
	}
	for name, p := range overrides {
		providers[name] = p
	}
	return NewRegistry(providers)
}
`
