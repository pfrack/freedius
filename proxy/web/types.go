package web

import (
	"github.com/pfrack/freedius/proxy"
)

// pageData is the common data passed to every page template.
type pageData struct {
	Active string
}

// dashboardData is the data for the redesigned dashboard page.
type dashboardData struct {
	pageData
	Health         healthStrip
	Alerts         []attentionAlert
	Rows           []routingTableRow
	ProviderHealth providerHealthSummary
	RecentActivity []activityRow
	// CurrentSeq is the latest event sequence number, used as the `?since=`
	// cursor for the SSE activity feed so the browser receives only live
	// events and skips the buffered replay.
	CurrentSeq int64
}

// healthStrip displays the router's high-level operational state.
type healthStrip struct {
	State            string // Healthy, Degraded
	Uptime           string
	Endpoint         string
	LastRequest      string // formatted timestamp or "No traffic"
	ErrorsLastHour   int64
	FallbacksLast24h int64
	TotalRequests    int64
}

// attentionAlert is a single alert shown in the conditional attention panel.
type attentionAlert struct {
	Severity string // error, warning
	Message  string
	Link     string
	Icon     string
}

// routingTableRow is a single row in the dashboard's compact routing table.
type routingTableRow struct {
	Name            string
	ProviderName    string
	Model           string
	FallbackSummary string // "provider / model" or "" if no fallback
	FallbackCount   int    // number of fallback entries
	StatusLabel     string // Healthy, Degraded, Error, Unknown, Key Missing
	StatusSlug      string // slug used for the badge CSS class (badge--status-<slug>)
	RequestCount    int64
	ErrorCount      int64
	FallbackEvents  int64
	LastActivity    string // formatted timestamp or "No traffic"
	EnvPresent      bool
}

// providerHealthSummary is the aggregated provider health section.
type providerHealthSummary struct {
	Total    int
	Healthy  int
	Degraded int
	Error    int
	Unknown  int
	Badges   []providerHealthBadge
}

// providerHealthBadge is a single provider badge in the health summary.
type providerHealthBadge struct {
	Name         string
	Status       string // healthy, degraded, error, unknown
	LastChecked  string
	MappingCount int
}

// activityRow is a single row in the recent activity feed.
type activityRow struct {
	Timestamp    string
	Mapping      string
	Route        string // "provider / model"
	FallbackUsed bool
	Latency      string
	Status       int
	StatusLabel  string // "OK" or error type
	LogsLink     string
}

// indexData is kept for backward compatibility with existing test assertions
// that reference this type. The dashboard handler now uses dashboardData.
type indexData = dashboardData

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
	// Provider is the active ?provider= filter ("" when no filter).
	Provider string
	// Mapping is the active ?mapping= filter ("" when no filter).
	Mapping string
	// Outcome is the active ?outcome= filter ("success", "error", or "").
	Outcome string
	// Fallback is the active ?fallback= filter ("true", "false", or "").
	Fallback string
}

// providerRow represents a single provider for template rendering.
type providerRow struct {
	Name             string
	Behavior         string
	BaseURL          string
	APIKeyEnv        string
	Protocol         string
	MappingCount     int
	Status           string // healthy, degraded, error, unknown
	LastSuccess      string
	LastError        string
	LastErrorMessage string
	RequestCount     int64
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
	Protocol     string
	BaseURL      string
}

// mappingRow represents a single mapping for template rendering.
type mappingRow struct {
	Name         string
	ProviderName string
	Model        string
	Protocol     string
	BaseURL      string
	Responder    int // responder index (0=primary; check HasResponder for validity)
	HasResponder bool
	Fallbacks    []fallbackEntry
	AddedAt      string
	EnvPresent   bool
}

// drawerData is the data for the mapping details drawer fragment, loaded
// via HTMX when a routing table row is clicked. Read-only view of one
// mapping enriched with live StatsCollector counters.
type drawerData struct {
	Name           string
	StatusLabel    string
	StatusSlug     string // slug used for the badge CSS class (badge--status-<slug>)
	Model          string
	ProviderName   string
	Protocol       string
	BaseURL        string
	Fallbacks      []drawerFallback
	RequestCount   int64
	ErrorCount     int64
	FallbackEvents int64
	LastActivity   string
	AddedAt        string
	EnvPresent     bool
}

// drawerFallback is a single fallback entry rendered inside the drawer.
// Kept distinct from fallbackEntry so the drawer's simpler read-only
// shape (Model + ProviderName only) isn't dragging in BaseURL/Protocol
// fields the drawer doesn't use.
type drawerFallback struct {
	Model        string
	ProviderName string
}

// mappingsData is the data for the mappings page.
type mappingsData struct {
	pageData
	Mappings  []mappingRow
	Providers []providerRow
	// TotalMappings is the count of mappings BEFORE filtering. Used by
	// the empty-state branch in the template to distinguish "no mappings
	// configured yet" (invite to add) from "filters excluded everything"
	// (invite to clear filters).
	TotalMappings int
	// Search is the active ?search= filter ("" when no filter). Pre-fills
	// the search input on the mappings routing table.
	Search string
	// ProviderFilter is the active ?provider= filter ("" when no filter).
	// Pre-selects the matching option in the provider dropdown.
	ProviderFilter string
	// StatusFilter is the active ?status= filter ("active", "inactive",
	// or "" when no filter). Pre-selects the matching option in the
	// status dropdown.
	StatusFilter string
	// HasFallbackFilter is the active ?has_fallback= filter ("true",
	// "false", or "" when no filter). Pre-checks the matching checkbox.
	HasFallbackFilter string
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
