package main

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fullSpec mirrors providers.yaml in the repo root. Tests use it directly
// so we don't have to read the file from disk in every test.
func fullSpec() Spec {
	return Spec{
		Providers: map[string]Provider{
			"nim": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://integrate.api.nvidia.com/v1/chat/completions",
				DefaultAPIKeyEnv: "NVIDIA_NIM_API_KEY",
				RequireBaseURL:   false,
				OpenAI: &OpenAIOptions{
					NoStreamUsage: true,
					PreSendHook:   "sanitizeNIMBody",
				},
			},
			"zen": {
				Behavior:         "mix",
				RewriteTo:        "mix",
				DefaultAPIKeyEnv: "OPENCODE_API_KEY",
				RequireBaseURL:   true,
			},
			"go": {
				Behavior:         "mix",
				RewriteTo:        "mix",
				DefaultAPIKeyEnv: "OPENCODE_API_KEY",
				RequireBaseURL:   true,
			},
			"custom": {
				Behavior:       "mix",
				RewriteTo:      "mix",
				RequireBaseURL: true,
			},
			"openai": {
				Behavior:       "openai",
				RequireBaseURL: true,
			},
			"anthropic": {
				Behavior:         "anthropic",
				DefaultAPIKeyEnv: "ANTHROPIC_API_KEY",
				RequireBaseURL:   true,
			},
			"mix": {
				Behavior:       "mix",
				RequireBaseURL: true,
			},
		},
	}
}

func TestGenerateConfig_CompilesAsGo(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("output is not go/format-clean:\n%v\n--- output ---\n%s", err, out)
	}
}

func TestGenerateConfig_KnownProvidersHas7Entries(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	block := extractVarBlock(t, src, "KnownProviders")
	for _, name := range []string{
		"nim", "zen", "go", "custom", "openai", "anthropic", "mix",
	} {
		// go/format aligns columns, so the actual text is `"name":{},`
		// with variable padding. Strip whitespace and check the core pattern.
		compact := stripWhitespace(block)
		if !strings.Contains(compact, `"`+name+`":{},`) {
			t.Errorf("KnownProviders missing %q; block:\n%s", name, block)
		}
	}
}

func TestGenerateConfig_DefaultsHas4Entries(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	// Only nim, zen, go, anthropic have defaults.
	for _, name := range []string{"nim", "zen", "go", "anthropic"} {
		if !strings.Contains(src, `"`+name+`": {`) {
			t.Errorf("knownProviderDefaults missing %q", name)
		}
	}
	// custom, openai, mix have no defaults — they must NOT appear as map keys.
	for _, name := range []string{"custom", "openai", "mix"} {
		// Look inside the defaults map only — we don't want false positives
		// from the requireBaseURL set, which also contains these names.
		defaultsBlock := extractDefaultsBlock(t, src)
		if strings.Contains(defaultsBlock, `"`+name+`": {`) {
			t.Errorf("knownProviderDefaults should NOT contain %q, got:\n%s", name, defaultsBlock)
		}
	}
}

func TestGenerateConfig_RequireBaseURLSet(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	setBlock := extractRequireBaseURLBlock(t, src)
	compact := stripWhitespace(setBlock)
	// openai, anthropic, mix, custom, zen, go require base_url.
	for _, name := range []string{"openai", "anthropic", "mix", "custom", "zen", "go"} {
		if !strings.Contains(compact, `"`+name+`":{},`) {
			t.Errorf("requireBaseURL missing %q; block:\n%s", name, setBlock)
		}
	}
	// nim has default_base_url, so it's NOT in requireBaseURL.
	if strings.Contains(compact, `"nim":{},`) {
		t.Errorf("requireBaseURL should NOT contain nim; got:\n%s", setBlock)
	}
}

func TestGenerateConfig_PresetProviders(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	fnBlock := extractFunctionBlock(t, src, "func PresetProviders")
	// Providers with default_api_key_env: nim, zen, go, anthropic.
	for _, name := range []string{"nim", "zen", "go", "anthropic"} {
		if !strings.Contains(fnBlock, `"`+name+`",`) {
			t.Errorf("PresetProviders missing %q", name)
		}
	}
	// No default key envs for custom, openai, mix.
	for _, name := range []string{"custom", "openai", "mix"} {
		if strings.Contains(fnBlock, `"`+name+`",`) {
			t.Errorf("PresetProviders should NOT contain %q", name)
		}
	}
}

func TestGenerateConfig_ApplyEntryDefaultsRewriteOrder(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	fnBlock := extractFunctionBlock(t, src, "func applyEntryDefaults")

	customIdx := strings.Index(fnBlock, `m.Provider == "custom"`)
	zenIdx := strings.Index(fnBlock, `m.Provider == "zen"`)
	goIdx := strings.Index(fnBlock, `m.Provider == "go"`)
	defaultsIdx := strings.Index(fnBlock, "knownProviderDefaults[m.Provider]")
	returnIdx := strings.Index(fnBlock, "return m")

	if customIdx == -1 {
		t.Fatal("custom rewrite missing from applyEntryDefaults")
	}
	if zenIdx == -1 || goIdx == -1 {
		t.Fatal("zen/go rewrites missing from applyEntryDefaults")
	}
	if defaultsIdx == -1 {
		t.Fatal("defaults lookup missing from applyEntryDefaults")
	}
	if returnIdx == -1 {
		t.Fatal("early return missing from applyEntryDefaults")
	}

	// Phase A (custom) MUST come before the defaults lookup AND before the
	// early return — otherwise the early return fires for "custom" before
	// it gets rewritten to "mix".
	if customIdx > defaultsIdx {
		t.Errorf(
			"custom rewrite must precede defaults lookup; custom=%d defaults=%d",
			customIdx,
			defaultsIdx,
		)
	}
	if customIdx > returnIdx {
		t.Errorf(
			"custom rewrite must precede early return; custom=%d return=%d",
			customIdx,
			returnIdx,
		)
	}

	// Phase B (zen/go) MUST come AFTER the defaults lookup AND AFTER the
	// default-application block — they need to inherit OPENCODE_API_KEY.
	if zenIdx < defaultsIdx {
		t.Errorf(
			"zen rewrite must come AFTER defaults lookup; zen=%d defaults=%d",
			zenIdx,
			defaultsIdx,
		)
	}
	if goIdx < defaultsIdx {
		t.Errorf(
			"go rewrite must come AFTER defaults lookup; go=%d defaults=%d",
			goIdx,
			defaultsIdx,
		)
	}
}

func TestGenerateConfig_OriginalProviderGuard(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	fnBlock := extractFunctionBlock(t, src, "func applyEntryDefaults")
	// Idempotency guard: OriginalProvider set only when empty.
	if !strings.Contains(fnBlock, `if m.OriginalProvider == ""`) {
		t.Errorf("applyEntryDefaults missing OriginalProvider idempotency guard:\n%s", fnBlock)
	}
}

func TestGenerateProxy_CompilesAsGo(t *testing.T) {
	out, err := GenerateProxy(fullSpec())
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("output is not go/format-clean:\n%v\n--- output ---\n%s", err, out)
	}
}

func TestGenerateProxy_EmitsNIMAdapter(t *testing.T) {
	out, err := GenerateProxy(fullSpec())
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	src := string(out)
	if !strings.Contains(src, "type NIMAdapter struct") {
		t.Errorf("NIMAdapter type missing; output:\n%s", src)
	}
	if !strings.Contains(
		src,
		"func NewNIMAdapter(logger *slog.Logger, streamTimeout time.Duration) *NIMAdapter",
	) {
		t.Errorf("NewNIMAdapter signature wrong; output:\n%s", src)
	}
	if !strings.Contains(src, "inner.translateOpts = translate.Opts{NoStreamUsage: true}") {
		t.Errorf("NIM NoStreamUsage=true not wired; output:\n%s", src)
	}
	if !strings.Contains(src, "inner.preSendHook = sanitizeNIMBody") {
		t.Errorf("NIM preSendHook not wired; output:\n%s", src)
	}
}

func TestGenerateProxy_NewDefaultRegistry(t *testing.T) {
	out, err := GenerateProxy(fullSpec())
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	src := string(out)
	// Signature — go/format may combine lines or keep them split.
	if !strings.Contains(src, "overrides map[string]Provider") {
		t.Errorf("NewDefaultRegistry missing overrides param; output:\n%s", src)
	}
	// All 4 runtime adapters wired with aligned formatting.
	compact := stripWhitespace(src)
	for _, want := range []string{
		`"nim":NewNIMAdapter(logger,streamTimeout)`,
		`"openai":NewOpenAICompatibleAdapterWithTimeout(logger,streamTimeout)`,
		`"anthropic":NewAnthropicCompatibleAdapter(logger,verboseErrors)`,
		`"mix":NewMixAdapter(logger,verboseErrors,streamTimeout)`,
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("registry missing entry %q in stripped output:\n%s", want, compact)
		}
	}
}

func TestGenerateProxy_DoesNotWireAliases(t *testing.T) {
	out, err := GenerateProxy(fullSpec())
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	src := string(out)
	// custom/zen/go are aliases that rewrite to mix — they must NOT have
	// runtime registry entries (otherwise the dispatcher would try to look
	// them up before applyDefaults runs, which it doesn't).
	for _, name := range []string{`"custom":`, `"zen":`, `"go":`} {
		if strings.Contains(src, name) {
			t.Errorf("registry must not wire alias %q; output:\n%s", name, src)
		}
	}
}

func TestLoadSpec_RealFile(t *testing.T) {
	// Verify the actual providers.yaml at repo root parses and has 7 entries.
	spec, err := loadSpec("../../providers.yaml")
	if err != nil {
		t.Fatalf("loadSpec: %v", err)
	}
	if got, want := len(spec.Providers), 7; got != want {
		t.Errorf("providers count: got %d, want %d", got, want)
	}
}

func TestGenerateConfig_FromRealFile_CompilesAndMatches(t *testing.T) {
	spec, err := loadSpec("../../providers.yaml")
	if err != nil {
		t.Fatalf("loadSpec: %v", err)
	}
	out, err := GenerateConfig(*spec)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("output is not go/format-clean:\n%v\n--- output ---\n%s", err, out)
	}
	block := extractVarBlock(t, string(out), "KnownProviders")
	compact := stripWhitespace(block)
	for _, name := range []string{
		"nim", "zen", "go", "custom", "openai", "anthropic", "mix",
	} {
		if !strings.Contains(compact, `"`+name+`":{},`) {
			t.Errorf("KnownProviders missing %q", name)
		}
	}
}

func TestGenerateProxy_FromRealFile_CompilesAndMatches(t *testing.T) {
	spec, err := loadSpec("../../providers.yaml")
	if err != nil {
		t.Fatalf("loadSpec: %v", err)
	}
	out, err := GenerateProxy(*spec)
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("output is not go/format-clean:\n%v\n--- output ---\n%s", err, out)
	}
}

// --- helpers ---

func extractDefaultsBlock(t *testing.T, src string) string {
	t.Helper()
	return extractVarBlock(t, src, "knownProviderDefaults")
}

func extractRequireBaseURLBlock(t *testing.T, src string) string {
	t.Helper()
	return extractVarBlock(t, src, "requireBaseURL")
}

func extractVarBlock(t *testing.T, src, varName string) string {
	t.Helper()
	idx := strings.Index(src, "var "+varName+" ")
	if idx == -1 {
		t.Fatalf("var %s not found", varName)
	}
	// Find the first '{' that is NOT inside a type constructor like
	// map[string]struct{}{.  Scan to the end of the declaration line,
	// then find the last '{' on that line — that's the map literal start.
	rest := src[idx:]
	nl := strings.IndexByte(rest, '\n')
	if nl == -1 {
		nl = len(rest)
	}
	declLine := rest[:nl]
	open := strings.LastIndexByte(declLine, '{')
	if open == -1 {
		t.Fatalf("var %s has no opening brace on declaration line", varName)
	}
	start := idx + open
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	t.Fatalf("var %s has no balanced close", varName)
	return ""
}

func extractFunctionBlock(t *testing.T, src, header string) string {
	t.Helper()
	idx := strings.Index(src, header)
	if idx == -1 {
		t.Fatalf("function %q not found", header)
	}
	open := strings.Index(src[idx:], "{")
	if open == -1 {
		t.Fatalf("function %q has no opening brace", header)
	}
	start := idx + open
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	t.Fatalf("function %q has no balanced close", header)
	return ""
}

// Smoke test: end-to-end generation matches a snapshot written to a temp dir.
// Not a golden-file test (those come in Phase 3), just proves the wiring works.
func TestGenerate_WritesAndReadsBack(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "providers.yaml")
	specData, err := os.ReadFile("../../providers.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, specData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Generate config to a file.
	cfgOut := filepath.Join(dir, "providers_gen.go")
	spec, err := loadSpec(specPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgBytes, err := GenerateConfig(*spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgOut, cfgBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Format again — should be idempotent.
	roundTrip, err := format.Source(cfgBytes)
	if err != nil {
		t.Fatalf("second go/format: %v", err)
	}
	if !bytes.Equal(cfgBytes, roundTrip) {
		t.Errorf("format.Source not idempotent:\nbefore:\n%s\nafter:\n%s", cfgBytes, roundTrip)
	}
}

// stripWhitespace removes all whitespace characters from s.  Useful for
// content checks against go/format output, which adds column-alignment padding.
func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
