package proxy

import (
	"os"
	"strings"
	"testing"
)

// TestPrivacyPolicyComments asserts the NFR-Privacy comment is present in the
// two files closest to body processing. The comment is the documented policy
// for future contributors; losing it would silently relax the privacy posture.
func TestPrivacyPolicyComments(t *testing.T) {
	candidates := []string{
		"proxy.go",
		"translate/anthropic_openai.go",
	}
	for _, rel := range candidates {
		path := rel // package-relative
		if _, err := os.Stat(path); err != nil {
			t.Skipf("file not found (run from repo root?): %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(data)
		if !strings.Contains(content, "DO NOT log request or response bodies") {
			t.Errorf("%s missing privacy-policy marker comment", path)
		}
		if !strings.Contains(content, "NFR-Privacy") {
			t.Errorf("%s missing NFR-Privacy reference", path)
		}
	}
}
