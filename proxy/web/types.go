package web

import (
	"github.com/pfrack/freedius/proxy"
)

// pageData is the common data passed to every page template.
type pageData struct {
	Active string
}

// indexData is the data for the index/dashboard page.
type indexData struct {
	pageData
	Uptime      string
	TotalEvents int64
	TotalLogs   int64
	Port        string
	Host        string
}

// logEntry represents a single log line for template rendering.
type logEntry struct {
	Level string
	Line  string
}

// logsData is the data for the logs page.
type logsData struct {
	pageData
	Entries []logEntry
	// Level is the active ?min= filter ("" when no filter). Used by logs.html
	// to highlight the selected option in the dropdown — see plan §2.6.
	Level string
}

// providerRow represents a single provider for template rendering.
type providerRow struct {
	Name         string
	Behavior     string
	BaseURL      string
	APIKeyEnv    string
	Protocol     string
	MappingCount int
}

// providersData is the data for the providers page.
type providersData struct {
	pageData
	Providers []providerRow
}

// fallbackEntry represents a single fallback provider/model pair.
type fallbackEntry struct {
	ProviderName string
	Model        string
}

// mappingRow represents a single mapping for template rendering.
type mappingRow struct {
	Name         string
	ProviderName string
	Model        string
	Fallbacks    []fallbackEntry
}

// mappingsData is the data for the mappings page.
type mappingsData struct {
	pageData
	Mappings  []mappingRow
	Providers []providerRow
}

// modelsData is the data for the models fragment template.
type modelsData struct {
	Provider        string
	Models          []proxy.ModelView
	FetchedAt       string
	Error           string
	Truncated       bool
	FetchInProgress bool
}
