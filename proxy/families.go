package proxy

import "regexp"

type familyPattern struct {
	name    string
	pattern *regexp.Regexp
}

var knownFamilies = []familyPattern{
	{name: "opus", pattern: regexp.MustCompile(`(?i)opus`)},
	{name: "sonnet", pattern: regexp.MustCompile(`(?i)sonnet`)},
	{name: "haiku", pattern: regexp.MustCompile(`(?i)haiku`)},
	{name: "auto", pattern: regexp.MustCompile(`(?i)auto`)},
	{name: "default", pattern: regexp.MustCompile(``)},
}

// ExtractFamily returns the first matching family keyword found in modelName,
// or ("", false) if none match. The known families are opus, sonnet, haiku,
// auto, and a "default" catch-all that matches any input. Callers that only
// want an explicit keyword should treat "default" as no-match.
func ExtractFamily(modelName string) (string, bool) {
	for _, f := range knownFamilies {
		if f.pattern.MatchString(modelName) {
			return f.name, true
		}
	}
	return "", false
}
