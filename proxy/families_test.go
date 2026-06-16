package proxy

import "testing"

func TestExtractFamily(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		want      string
		wantFound bool
	}{
		// Canonical examples
		{name: "opus canonical", model: "claude-opus-4-1", want: "opus", wantFound: true},
		{name: "sonnet canonical", model: "claude-sonnet-4-6", want: "sonnet", wantFound: true},
		{name: "haiku canonical", model: "claude-haiku-3-5", want: "haiku", wantFound: true},
		{name: "auto canonical", model: "auto", want: "auto", wantFound: true},
		// Case-insensitive
		{name: "opus uppercase", model: "CLAUDE-OPUS-4-1", want: "opus", wantFound: true},
		{name: "sonnet mixed case", model: "Claude-Sonnet-4-6", want: "sonnet", wantFound: true},
		// Priority resolution: sonnet beats haiku (order in knownFamilies)
		{name: "priority sonnet over haiku", model: "claude-haiku-sonnet-2024", want: "sonnet", wantFound: true},
		// default matches anything
		{name: "default catches unknown", model: "claude-future-model-2026", want: "default", wantFound: true},
		{name: "default empty string", model: "", want: "default", wantFound: true},
		// Additional variants
		{name: "opus variant", model: "claude-3-opus-20240229", want: "opus", wantFound: true},
		{name: "sonnet variant", model: "claude-3-sonnet-20240229", want: "sonnet", wantFound: true},
		{name: "haiku variant", model: "claude-3-haiku-20240307", want: "haiku", wantFound: true},
		{name: "haiku in company name", model: "haiku-company-model-v2", want: "haiku", wantFound: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := extractFamily(tt.model)
			if found != tt.wantFound {
				t.Errorf("extractFamily(%q) found = %v, want %v", tt.model, found, tt.wantFound)
			}
			if got != tt.want {
				t.Errorf("extractFamily(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}
