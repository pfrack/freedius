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
				DefaultAPIKeyEnv: "OPENCODE_API_KEY",
				RequireBaseURL:   true,
				OpenAI:           &OpenAIOptions{NoStreamUsage: true},
			},
			"go": {
				Behavior:         "mix",
				DefaultAPIKeyEnv: "OPENCODE_API_KEY",
				RequireBaseURL:   true,
				OpenAI:           &OpenAIOptions{NoStreamUsage: true},
			},
			"custom": {
				Behavior:       "mix",
				RequireBaseURL: true,
				OpenAI:         &OpenAIOptions{NoStreamUsage: true},
			},
			"openai": {
				Behavior:       "openai",
				RequireBaseURL: true,
			},
			"anthropic": {
				Behavior:         "anthropic",
				DefaultBaseURL:   "https://api.anthropic.com/v1/messages",
				DefaultAPIKeyEnv: "ANTHROPIC_API_KEY",
				RequireBaseURL:   false,
			},
			"mix": {
				Behavior:       "mix",
				RequireBaseURL: true,
				OpenAI:         &OpenAIOptions{NoStreamUsage: true},
			},
			"google": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
				DefaultAPIKeyEnv: "GEMINI_API_KEY",
				RequireBaseURL:   false,
				OpenAI:           &OpenAIOptions{NoStreamUsage: true},
			},
			"mistral": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://api.mistral.ai/v1/chat/completions",
				DefaultAPIKeyEnv: "MISTRAL_API_KEY",
				RequireBaseURL:   false,
			},
			"deepseek": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://api.deepseek.com/chat/completions",
				DefaultAPIKeyEnv: "DEEPSEEK_API_KEY",
				RequireBaseURL:   false,
			},
			"groq": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://api.groq.com/openai/v1/chat/completions",
				DefaultAPIKeyEnv: "GROQ_API_KEY",
				RequireBaseURL:   false,
			},
			"together": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://api.together.xyz/v1/chat/completions",
				DefaultAPIKeyEnv: "TOGETHER_API_KEY",
				RequireBaseURL:   false,
			},
			"fireworks": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://api.fireworks.ai/inference/v1/chat/completions",
				DefaultAPIKeyEnv: "FIREWORKS_API_KEY",
				RequireBaseURL:   false,
			},
			"cohere": {
				Behavior:         "openai",
				DefaultBaseURL:   "https://api.cohere.com/compatibility/v1/chat/completions",
				DefaultAPIKeyEnv: "COHERE_API_KEY",
				RequireBaseURL:   false,
			},
			"ollama": {
				Behavior:       "openai",
				DefaultBaseURL: "http://localhost:11434/v1/chat/completions",
				RequireBaseURL: false,
				OpenAI:         &OpenAIOptions{NoStreamUsage: true},
			},
			"lmstudio": {
				Behavior:       "openai",
				DefaultBaseURL: "http://localhost:1234/v1/chat/completions",
				RequireBaseURL: false,
				OpenAI:         &OpenAIOptions{NoStreamUsage: true},
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

func TestGenerateConfig_ProviderDefaultsHas16Entries(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	if !strings.Contains(src, "var providerDefaults = map[string]Provider{") {
		t.Fatalf("providerDefaults map missing; output:\n%s", src)
	}
	for _, name := range []string{
		"nim", "zen", "go", "custom", "openai", "anthropic", "mix",
		"google", "mistral", "deepseek", "groq", "together", "fireworks", "cohere",
		"ollama", "lmstudio",
	} {
		// Each entry uses a quoted name as a map key.
		if !strings.Contains(src, `"`+name+`": {`) {
			t.Errorf("providerDefaults missing %q; output:\n%s", name, src)
		}
	}
}

func TestGenerateConfig_ProviderDefaultsNoRewriteTo(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	// The new schema has no rewrite_to field; generated code must not
	// reference it.
	if strings.Contains(src, "rewrite_to") || strings.Contains(src, "rewriteTo") {
		t.Errorf("generated code should not reference rewrite_to; output:\n%s", src)
	}
	// No applyEntryDefaults function (rewrite machinery deleted).
	if strings.Contains(src, "func applyEntryDefaults") {
		t.Errorf("applyEntryDefaults must not be generated; output:\n%s", src)
	}
}

func TestGenerateConfig_SupportsCountTokens(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	// anthropic supports count_tokens; openai and mix (without /v1/messages
	// base_url) do not.
	if !strings.Contains(src, `"anthropic": {`) {
		t.Fatalf("anthropic entry missing; output:\n%s", src)
	}
	// The anthropic block must set SupportsCountTokens: true.
	anthIdx := strings.Index(src, `"anthropic": {`)
	if anthIdx == -1 {
		t.Fatal("anthropic block not found")
	}
	// Find the matching close brace at the same depth.
	depth := 0
	end := anthIdx
scanAnth:
	for i := anthIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				break scanAnth
			}
		}
	}
	anthBlock := src[anthIdx : end+1]
	if !strings.Contains(anthBlock, "SupportsCountTokens: true") {
		t.Errorf("anthropic should have SupportsCountTokens: true, got block:\n%s", anthBlock)
	}
	// nim (openai behavior) should have SupportsCountTokens: false.
	nimIdx := strings.Index(src, `"nim": {`)
	if nimIdx == -1 {
		t.Fatal("nim block not found")
	}
	depth = 0
	end = nimIdx
scanNim:
	for i := nimIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				break scanNim
			}
		}
	}
	nimBlock := src[nimIdx : end+1]
	if !strings.Contains(nimBlock, "SupportsCountTokens: false") {
		t.Errorf("nim should have SupportsCountTokens: false, got block:\n%s", nimBlock)
	}
}

func TestGenerateConfig_NIMCarriesOpenAIOptions(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	nimIdx := strings.Index(src, `"nim": {`)
	if nimIdx == -1 {
		t.Fatal("nim block not found")
	}
	depth := 0
	end := nimIdx
scanNim:
	for i := nimIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				break scanNim
			}
		}
	}
	nimBlock := src[nimIdx : end+1]
	for _, want := range []string{
		"OpenAI: &OpenAIOptions{",
		"NoStreamUsage: true",
		`PreSendHook:`,
		`"sanitizeNIMBody"`,
	} {
		if !strings.Contains(nimBlock, want) {
			t.Errorf("nim OpenAI options missing %q; block:\n%s", want, nimBlock)
		}
	}
}

func TestGenerateConfig_MixProvidersCarryNoStreamUsage(t *testing.T) {
	out, err := GenerateConfig(fullSpec())
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	src := string(out)
	for _, name := range []string{"zen", "go", "custom", "mix"} {
		key := `"` + name + `": {`
		idx := strings.Index(src, key)
		if idx == -1 {
			t.Fatalf("%s block not found", name)
		}
		depth := 0
		end := idx
	scan:
		for i := idx; i < len(src); i++ {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
					break scan
				}
			}
		}
		block := src[idx : end+1]
		if !strings.Contains(block, "OpenAI: &OpenAIOptions{") {
			t.Errorf("%s should carry OpenAI options; block:\n%s", name, block)
		}
		if !strings.Contains(block, "NoStreamUsage: true") {
			t.Errorf("%s should set NoStreamUsage: true; block:\n%s", name, block)
		}
	}
}

func TestGenerateProxy_CompilesAsGo(t *testing.T) {
	out, err := GenerateProxy()
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("output is not go/format-clean:\n%v\n--- output ---\n%s", err, out)
	}
}

func TestGenerateProxy_OmitsPerProviderWrappers(t *testing.T) {
	out, err := GenerateProxy()
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	src := string(out)
	for _, typ := range []string{
		"type GoogleAdapter struct",
		"type LmstudioAdapter struct",
		"type OllamaAdapter struct",
		"type NIMAdapter struct",
	} {
		if strings.Contains(src, typ) {
			t.Errorf("per-provider wrapper %q must not be generated; output:\n%s", typ, src)
		}
	}
}

func TestGenerateProxy_NewDefaultRegistry(t *testing.T) {
	out, err := GenerateProxy()
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	src := string(out)
	if !strings.Contains(src, "overrides map[string]Provider") {
		t.Errorf("NewDefaultRegistry missing overrides param; output:\n%s", src)
	}
	// All 4 runtime adapters wired with aligned formatting.
	compact := stripWhitespace(src)
	for _, want := range []string{
		`"nim":NewOpenAICompatibleAdapterWithTimeout(logger,streamTimeout)`,
		`"openai":NewOpenAICompatibleAdapterWithTimeout(logger,streamTimeout)`,
		`"anthropic":NewAnthropicCompatibleAdapterWithTimeout(logger,verboseErrors,streamTimeout)`,
		`"mix":NewMixAdapter(logger,verboseErrors,streamTimeout)`,
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("registry missing entry %q in stripped output:\n%s", want, compact)
		}
	}
}

func TestGenerateProxy_NoPerProviderHandleWrappers(t *testing.T) {
	out, err := GenerateProxy()
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	src := string(out)
	// The wrappers (and their embedded Handle methods) are no longer
	// generated; per-request behavior is driven by config at Handle time.
	if strings.Contains(src, ") Handle(") {
		t.Errorf("generated proxy should not contain per-provider Handle wrappers; output:\n%s", src)
	}
	if !strings.Contains(src, "func NewDefaultRegistry") {
		t.Errorf("generated proxy must contain NewDefaultRegistry; output:\n%s", src)
	}
}

func TestLoadSpec_RealFile(t *testing.T) {
	spec, err := loadSpec("../../providers.yaml")
	if err != nil {
		t.Fatalf("loadSpec: %v", err)
	}
	if got, want := len(spec.Providers), 18; got != want {
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
	src := string(out)
	for _, name := range []string{
		"nim", "zen", "go", "custom", "openai", "anthropic", "mix",
		"google", "mistral", "deepseek", "groq", "together", "fireworks", "cohere",
		"ollama", "lmstudio",
	} {
		if !strings.Contains(src, `"`+name+`": {`) {
			t.Errorf("providerDefaults missing %q", name)
		}
	}
}

func TestGenerateProxy_FromRealFile_CompilesAndMatches(t *testing.T) {
	out, err := GenerateProxy()
	if err != nil {
		t.Fatalf("GenerateProxy: %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("output is not go/format-clean:\n%v\n--- output ---\n%s", err, out)
	}
}

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

// stripWhitespace removes all whitespace characters from s. Useful for
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
