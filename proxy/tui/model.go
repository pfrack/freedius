// Package tui implements the Bubble Tea terminal dashboard for freedius.
// It provides live request monitoring, provider health display, and
// read-only config inspection through three tabbed views.
package tui

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/internal/envinject"
	"github.com/pfrack/freedius/proxy"
)

type requestEventMsg proxy.RequestEvent

type logEntryMsg proxy.LogEntry

type statsData struct {
	startTime     time.Time
	totalRequests int
	errorCount    int
	message       string
}

type ringBuffer[T any] struct {
	buf  []T
	head int
	size int
	cap  int
}

func newRingBuffer[T any](capacity int) *ringBuffer[T] {
	return &ringBuffer[T]{
		buf: make([]T, capacity),
		cap: capacity,
	}
}

func (rb *ringBuffer[T]) push(e T) {
	rb.buf[rb.head] = e
	rb.head = (rb.head + 1) % rb.cap
	if rb.size < rb.cap {
		rb.size++
	}
}

func (rb *ringBuffer[T]) all() []T {
	if rb.size == 0 {
		return nil
	}
	start := (rb.head - rb.size + rb.cap) % rb.cap
	end := start + rb.size
	if end <= rb.cap {
		return rb.buf[start:end]
	}
	result := make([]T, rb.size)
	for i := 0; i < rb.size; i++ {
		result[i] = rb.buf[(start+i)%rb.cap]
	}
	return result
}

type providerSelectedMsg string

type formSubmittedMsg struct{}

// Dashboard is the top-level Bubble Tea model for the freedius TUI.
// It owns the event subscription channel, ring buffer, tabs, and stats.
type Dashboard struct {
	activeTab  int
	events     <-chan proxy.RequestEvent
	logs       <-chan proxy.LogEntry
	logBuffer  *ringBuffer[proxy.LogEntry]
	styleBody  bool
	config     *config.Config
	registry   *proxy.Registry
	dispatcher *proxy.Dispatcher
	stats      statsData
	width      int
	height     int
	quitting   bool

	host            string
	port            int
	verboseErrors   bool
	currentLogLevel LogFilter

	formMode       int
	formFields     []textinput.Model
	formFocus      int
	formKind       string
	formEntryName  string
	fieldErrors    map[int]string
	showPicker     bool
	picker         *providerPicker
	showHelp       bool
	cfgPath        string
	configCursor   int
	providerScroll int
	logScroll      int
	formError      string

	styles       Styles
	isDark       bool
	currentTheme *Theme
}

// NewDashboard creates a new Dashboard model subscribed to the given event
// channel, configuration, and adapter registry. host, port, and
// verboseErrors are stored for runtime use by the TUI shortcuts (Ctrl+E
// toggles verbose errors; Ctrl+S uses host/port to install the shell RC).
//
// cfg, reg, and dispatcher are required. Passing nil panics, matching the
// convention in NewDispatcher and NewRegistry — silent-nil masks bugs that
// only surface when the renderer or shortcut tries to dereference.
func NewDashboard(
	events <-chan proxy.RequestEvent,
	logs <-chan proxy.LogEntry,
	cfg *config.Config,
	reg *proxy.Registry,
	dispatcher *proxy.Dispatcher,
	cfgPath string,
	host string,
	port int,
	verboseErrors bool,
	themeName string,
) *Dashboard {
	if cfg == nil {
		panic("tui: nil config")
	}
	if reg == nil {
		panic("tui: nil registry")
	}
	if dispatcher == nil {
		panic("tui: nil dispatcher")
	}
	isDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	theme := resolveTheme(themeName)
	return &Dashboard{
		activeTab:       tabLog,
		events:          events,
		logs:            logs,
		logBuffer:       newRingBuffer[proxy.LogEntry](1000),
		currentLogLevel: filterAll,
		config:          cfg,
		registry:        reg,
		dispatcher:      dispatcher,
		cfgPath:         cfgPath,
		host:            host,
		port:            port,
		verboseErrors:   verboseErrors,
		stats: statsData{
			startTime: time.Now(),
		},
		isDark:       isDark,
		currentTheme: theme,
		styles:       NewStyles(theme.Palette, isDark),
	}
}

// Init returns the initial command that starts listening for proxy events
// and log entries.
func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(waitForEvent(d.events), waitForLog(d.logs))
}

// Update handles incoming messages: key presses for tab switching and quit,
// window resize events, and request events from the proxy event bus.
func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// --- Form-specific messages ---
	case providerSelectedMsg:
		provider := string(msg)
		// Identify which field is focused by label, and write the selected
		// value into that field.
		labels := fieldLabelsForMode(d.formMode)
		if d.formFocus >= 0 && d.formFocus < len(labels) {
			fieldName := labels[d.formFocus]
			if fieldName == "provider" || fieldName == "behavior" {
				if d.formFocus < len(d.formFields) {
					d.formFields[d.formFocus].SetValue(provider)
				}
			}
		}
		d.showPicker = false
		return d, nil

	case formSubmittedMsg:
		d.submitForm()
		return d, nil

	// --- Key presses ---
	case tea.KeyPressMsg:
		if d.showHelp {
			switch msg.String() {
			case "?", "esc":
				d.showHelp = false
			}
			return d, nil
		}

		// Esc always quits when no form is active.
		if d.formMode == formNone && msg.String() == "esc" {
			d.quitting = true
			return d, tea.Quit
		}

		// --- Delete confirm mode key handling ---
		if d.formMode == formDeleteConfirm {
			return d.handleDeleteConfirmKeyPress(msg)
		}

		// --- Form mode key handling ---
		if d.formMode != formNone {
			return d.handleFormKeyPress(msg)
		}

		// --- Normal (tab) mode key handling ---
		return d.handleTabModeKeyPress(msg)

	// --- Window resize ---
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil

	// --- Request events ---
	case requestEventMsg:
		ev := proxy.RequestEvent(msg)
		d.stats.totalRequests++
		if ev.Status >= 400 {
			d.stats.errorCount++
		}
		d.stats.message = ""
		return d, waitForEvent(d.events)

	// --- Log entries ---
	case logEntryMsg:
		d.logBuffer.push(proxy.LogEntry(msg))
		return d, waitForLog(d.logs)
	}
	return d, nil
}

func (d *Dashboard) installShellRC() {
	home, err := os.UserHomeDir()
	if err != nil {
		d.stats.message = fmt.Sprintf("Shell install failed: %v", err)
		return
	}
	shell := os.Getenv("SHELL")
	// WriteShellRC returns ("<path>", nil) on success; on already-installed it
	// returns the path with an error so the caller can decide. Recognize the
	// already-installed case as a success/no-op rather than a failure.
	if _, err := envinject.WriteShellRC(home, shell, d.host, d.port, false, false); err != nil {
		if strings.Contains(err.Error(), "already installed") {
			d.stats.message = "Shell RC already installed ✓"
			return
		}
		d.stats.message = fmt.Sprintf("Shell install failed: %v", err)
		return
	}
	d.stats.message = "Shell RC updated ✓"
}

func (d *Dashboard) handleTabModeKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?":
		d.showHelp = true
		return d, nil
	case "q", "ctrl+c":
		d.quitting = true
		return d, tea.Quit
	case "1":
		d.activeTab = tabLog
		return d, nil
	case "2":
		d.activeTab = tabProviders
		return d, nil
	case "3":
		d.activeTab = tabConfig
		return d, nil
	case "tab":
		d.activeTab = (d.activeTab + 1) % 3
		return d, nil
	case "shift+tab":
		d.activeTab = (d.activeTab + 2) % 3
		return d, nil
	case "up", "k":
		d.scrollUp()
		return d, nil
	case "down", "j":
		d.scrollDown()
		return d, nil
	case "e", "enter":
		if d.activeTab == tabConfig {
			d.openEditForm()
		}
		return d, nil
	case "a":
		if d.activeTab == tabConfig {
			d.openAddMappingForm()
		}
		return d, nil
	case "p":
		if d.activeTab == tabConfig {
			d.openAddProviderForm()
		}
		return d, nil
	case "d":
		if d.activeTab == tabConfig {
			all := collectAllEntries(d.config)
			if d.configCursor >= 0 && d.configCursor < len(all) {
				entry := all[d.configCursor]
				d.formEntryName = entry.name
				d.formKind = entry.kind
				d.formMode = formDeleteConfirm
			}
		}
		return d, nil
	case "ctrl+e":
		d.toggleVerboseErrors()
		return d, nil
	case "l":
		d.cycleLogLevel()
		return d, nil
	case "ctrl+s":
		if d.activeTab != tabConfig {
			return d, nil
		}
		d.installShellRC()
		return d, nil
	}
	return d, nil
}

// scrollUp moves the active tab's cursor / scroll window one step toward
// older entries. For tabs without scroll state (Log uses scroll, Providers
// uses scroll, Config uses cursor) the per-tab helper applies.
func (d *Dashboard) scrollUp() {
	switch d.activeTab {
	case tabConfig:
		d.configCursor--
		if d.configCursor < 0 {
			d.configCursor = 0
		}
	case tabProviders:
		d.providerScroll++
	case tabLog:
		d.logScroll++
	}
}

// scrollDown moves the active tab's cursor / scroll window one step toward
// newer entries.
func (d *Dashboard) scrollDown() {
	switch d.activeTab {
	case tabConfig:
		all := collectAllEntries(d.config)
		d.configCursor++
		if d.configCursor >= len(all) {
			d.configCursor = len(all) - 1
		}
	case tabProviders:
		if d.providerScroll > 0 {
			d.providerScroll--
		}
	case tabLog:
		if d.logScroll > 0 {
			d.logScroll--
		}
	}
}

// toggleVerboseErrors flips the verbose-errors flag on both the dashboard and
// the dispatcher, and surfaces the new state in the status bar.
func (d *Dashboard) toggleVerboseErrors() {
	d.verboseErrors = !d.verboseErrors
	if d.dispatcher != nil {
		d.dispatcher.SetVerboseErrors(d.verboseErrors)
	}
	if d.verboseErrors {
		d.stats.message = "Verbose errors: ON"
	} else {
		d.stats.message = "Verbose errors: OFF"
	}
}

// cycleLogLevel advances the active log level filter through the cycle
// All → Debug → Info → Warn → Error → All and resets the scroll position.
func (d *Dashboard) cycleLogLevel() {
	for i, f := range logFilterCycle {
		if f.Label == d.currentLogLevel.Label {
			next := (i + 1) % len(logFilterCycle)
			d.currentLogLevel = logFilterCycle[next]
			d.logScroll = 0
			return
		}
	}
	// Fallback: start from All.
	d.currentLogLevel = filterAll
	d.logScroll = 0
}

func (d *Dashboard) handleFormKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if d.showPicker && d.picker != nil {
		cmd, done := d.picker.Update(msg)
		if done {
			if p := d.picker.SelectedProvider(); p != "" {
				return d, func() tea.Msg { return providerSelectedMsg(p) }
			}
			d.showPicker = false
		}
		return d, cmd
	}

	switch msg.String() {
	case "tab":
		d.formFocus = (d.formFocus + 1) % len(d.formFields)
		d.updateFormFocus()
		return d, nil
	case "shift+tab":
		d.formFocus = (d.formFocus - 1 + len(d.formFields)) % len(d.formFields)
		d.updateFormFocus()
		return d, nil
	case "enter":
		if d.formFocus >= 0 && d.formFocus < len(d.formFields) {
			labels := fieldLabelsForMode(d.formMode)
			if d.formFocus < len(labels) {
				fieldName := labels[d.formFocus]
				if fieldName == "provider" && d.formMode == formAddMapping {
					providers := d.config.ProvidersSnapshot()
					names := sortedConfiguredProviderNames(providers)
					d.picker = newProviderPicker(names, providers, d.styles)
					d.showPicker = true
					return d, nil
				}
				if fieldName == "behavior" &&
					(d.formMode == formAddProvider || d.formMode == formEditProvider) {
					d.picker = newBehaviorPicker(d.styles)
					d.showPicker = true
					return d, nil
				}
			}
		}
		d.fieldErrors = d.validateForm()
		if len(d.fieldErrors) == 0 {
			return d, func() tea.Msg { return formSubmittedMsg{} }
		}
		return d, nil
	case "esc":
		d.resetForm()
		return d, nil
	}

	if d.formFocus >= 0 && d.formFocus < len(d.formFields) {
		var cmd tea.Cmd
		d.formFields[d.formFocus], cmd = d.formFields[d.formFocus].Update(msg)
		return d, cmd
	}
	return d, nil
}

func (d *Dashboard) handleDeleteConfirmKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		d.config.Lock()
		defer d.config.Unlock()
		switch d.formKind {
		case "provider":
			old := d.config.Providers[d.formEntryName]
			delete(d.config.Providers, d.formEntryName)
			if d.cfgPath != "" {
				if err := d.config.Save(d.cfgPath); err != nil {
					d.config.Providers[d.formEntryName] = old
					d.formError = fmt.Sprintf("save failed: %v", err)
				}
			}
		case "mapping":
			old := d.config.Mappings[d.formEntryName]
			delete(d.config.Mappings, d.formEntryName)
			if d.cfgPath != "" {
				if err := d.config.Save(d.cfgPath); err != nil {
					d.config.Mappings[d.formEntryName] = old
					d.formError = fmt.Sprintf("save failed: %v", err)
				}
			}
		}
		d.resetForm()
	case "n", "esc":
		d.resetForm()
	}
	return d, nil
}

// View renders the full dashboard: tab bar, active tab content, and stats bar.
func (d *Dashboard) View() tea.View {
	if d.quitting {
		v := tea.NewView("")
		v.AltScreen = false
		return v
	}
	width := d.width
	if width <= 0 {
		width = 80
	}
	height := d.height
	if height <= 0 {
		height = 24
	}
	// Reserve 3 rows for the chrome: 1 row for the stats bar + 1 row for the
	// tab labels + 1 row for the tab bar's bottom border. The count is
	// symmetric regardless of whether stats is at the top or bottom, so this
	// budget works for both the old (tabs → body → stats) and new
	// (stats → tabs → body) layouts.
	bodyHeight := height - 3

	var content string
	if d.formMode != formNone {
		content = renderForm(d, width, bodyHeight)
	} else {
		d.styleBody = d.activeTab != tabLog
		switch d.activeTab {
		case tabLog:
			content = renderLogTab(d.logBuffer.all(), width, bodyHeight, d.logScroll, d.currentLogLevel)
		case tabProviders:
			content = renderProvidersTab(d.config, width, bodyHeight, d.providerScroll, d.styles)
		case tabConfig:
			content = renderConfigTab(d.config, d.configCursor, width, bodyHeight, d.styles)
		default:
			content = fmt.Sprintf("Unknown tab: %d", d.activeTab)
		}
	}

	stats := renderStatsBar(d.stats, width, d.styles)
	tabs := renderTabs(d.activeTab, width, d.currentLogLevel, d.styles)
	var body string
	if d.styleBody {
		body = d.styles.WindowStyle.Width(max(width-2, 0)).Render(content)
	} else {
		body = content
	}

	result := fmt.Sprintf("%s\n%s\n%s", stats, tabs, body)

	if d.showHelp {
		modal := renderHelpModal(width, d.styles)
		result = overlayModal(result, modal, width, height, d.styles.OverlayBgStyle)
	}

	v := tea.NewView(result)
	v.AltScreen = true
	return v
}

func waitForEvent(ch <-chan proxy.RequestEvent) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return requestEventMsg(ev)
	}
}

func waitForLog(ch <-chan proxy.LogEntry) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		entry, ok := <-ch
		if !ok {
			return nil
		}
		return logEntryMsg(entry)
	}
}

func (d *Dashboard) newFormField(value, placeholder string) textinput.Model {
	ti := textinput.New()
	ti.SetValue(value)
	ti.Placeholder = placeholder
	return ti
}

func (d *Dashboard) updateFormFocus() {
	for i := range d.formFields {
		if i == d.formFocus {
			d.formFields[i].Focus()
		} else {
			d.formFields[i].Blur()
		}
	}
}

func (d *Dashboard) openEditForm() {
	all := collectAllEntries(d.config)
	if d.configCursor < 0 || d.configCursor >= len(all) {
		return
	}
	entry := all[d.configCursor]
	d.formKind = entry.kind
	d.formEntryName = entry.name

	switch entry.kind {
	case "provider":
		p := entry.provider
		d.formFields = []textinput.Model{
			d.newFormField(entry.name, "provider name"),
			d.newFormField(p.Behavior, "behavior"),
			d.newFormField(p.DefaultBaseURL, "base_url"),
			d.newFormField(p.DefaultAPIKeyEnv, "api_key_env"),
			d.newFormField(p.AnthropicVersion, "anthropic_version"),
		}
		d.formMode = formEditProvider
	case "mapping":
		m := entry.mapping
		d.formFields = []textinput.Model{
			d.newFormField(entry.name, "mapping name"),
			d.newFormField(m.ProviderName, "provider"),
			d.newFormField(m.ModelString, "model"),
		}
		d.formMode = formEditMapping
	}
	d.formFocus = 0
	d.updateFormFocus()
	d.fieldErrors = nil
	d.formError = ""
	d.showPicker = false
}

func (d *Dashboard) openAddProviderForm() {
	d.formKind = "provider"
	d.formEntryName = ""

	d.formFields = []textinput.Model{
		d.newFormField("", "provider name"),
		d.newFormField("", "behavior"),
		d.newFormField("", "base_url"),
		d.newFormField("", "api_key_env"),
		d.newFormField("", "anthropic_version"),
	}
	d.formFocus = 0
	d.updateFormFocus()
	d.fieldErrors = nil
	d.formError = ""
	d.showPicker = false
	d.formMode = formAddProvider
}

func (d *Dashboard) openAddMappingForm() {
	d.formKind = "mapping"
	d.formEntryName = ""

	d.formFields = []textinput.Model{
		d.newFormField("", "mapping name"),
		d.newFormField("", "provider"),
		d.newFormField("", "model"),
	}
	d.formFocus = 0
	d.updateFormFocus()
	d.fieldErrors = nil
	d.formError = ""
	d.showPicker = false
	d.formMode = formAddMapping
}

func (d *Dashboard) resetForm() {
	d.formMode = formNone
	d.formFields = nil
	d.formFocus = 0
	d.formKind = ""
	d.formEntryName = ""
	d.fieldErrors = nil
	d.showPicker = false
	d.picker = nil
	d.formError = ""
}

func (d *Dashboard) validateForm() map[int]string {
	errs := make(map[int]string)

	name := strings.TrimSpace(d.formFields[0].Value())
	if name == "" {
		errs[0] = "entry name is required"
	}
	if strings.ContainsAny(name, "\r\n:") {
		errs[0] = "entry name must not contain CR, LF, or colon"
	}

	switch d.formMode {
	case formEditProvider, formAddProvider:
		behavior := strings.TrimSpace(d.formFields[1].Value())
		switch behavior {
		case "openai", "anthropic", "mix":
			// valid
		default:
			errs[1] = "behavior must be one of: openai, anthropic, mix"
		}
		baseURL := strings.TrimSpace(d.formFields[2].Value())
		if baseURL != "" {
			if u, err := url.Parse(baseURL); err != nil ||
				(u.Scheme != "http" && u.Scheme != "https") {
				errs[2] = "base_url must be a valid http(s) URL"
			}
		}
		apiKeyEnv := strings.TrimSpace(d.formFields[3].Value())
		if apiKeyEnv != "" && strings.ContainsAny(apiKeyEnv, "\r\n=") {
			errs[3] = "api_key_env must not contain CR, LF, or ="
		}
	case formEditMapping, formAddMapping:
		providerName := strings.TrimSpace(d.formFields[1].Value())
		if providerName == "" {
			errs[1] = "provider is required"
		} else if !d.config.HasProvider(providerName) {
			errs[1] = fmt.Sprintf("unknown provider %q (add it first with 'p')", providerName)
		}
		modelStr := strings.TrimSpace(d.formFields[2].Value())
		if modelStr == "" {
			errs[2] = "model is required"
		} else if strings.ContainsAny(modelStr, "\r\n:") {
			errs[2] = "model must not contain CR, LF, or colon"
		}
	}

	return errs
}

func (d *Dashboard) submitForm() {
	name := strings.TrimSpace(d.formFields[0].Value())

	d.config.Lock()
	defer d.config.Unlock()

	switch d.formMode {
	case formEditProvider:
		p := d.collectProviderFromForm()
		oldP, hadOld := d.config.Providers[d.formEntryName]
		if d.config.Providers == nil {
			d.config.Providers = map[string]config.Provider{}
		}
		delete(d.config.Providers, d.formEntryName)
		d.config.Providers[name] = p
		if d.cfgPath != "" {
			if err := d.config.Save(d.cfgPath); err != nil {
				delete(d.config.Providers, name)
				if hadOld {
					d.config.Providers[d.formEntryName] = oldP
				}
				d.formError = fmt.Sprintf("save failed: %v", err)
			}
		}
	case formAddProvider:
		p := d.collectProviderFromForm()
		if d.config.Providers == nil {
			d.config.Providers = map[string]config.Provider{}
		}
		d.config.Providers[name] = p
		if d.cfgPath != "" {
			if err := d.config.Save(d.cfgPath); err != nil {
				delete(d.config.Providers, name)
				d.formError = fmt.Sprintf("save failed: %v", err)
			}
		}
	case formEditMapping:
		m := d.collectMappingFromForm()
		oldM, hadOld := d.config.Mappings[d.formEntryName]
		if d.config.Mappings == nil {
			d.config.Mappings = map[string]config.Mapping{}
		}
		delete(d.config.Mappings, d.formEntryName)
		d.config.Mappings[name] = m
		if d.cfgPath != "" {
			if err := d.config.Save(d.cfgPath); err != nil {
				delete(d.config.Mappings, name)
				if hadOld {
					d.config.Mappings[d.formEntryName] = oldM
				}
				d.formError = fmt.Sprintf("save failed: %v", err)
			}
		}
	case formAddMapping:
		m := d.collectMappingFromForm()
		if d.config.Mappings == nil {
			d.config.Mappings = map[string]config.Mapping{}
		}
		d.config.Mappings[name] = m
		if d.cfgPath != "" {
			if err := d.config.Save(d.cfgPath); err != nil {
				delete(d.config.Mappings, name)
				d.formError = fmt.Sprintf("save failed: %v", err)
			}
		}
	}

	d.resetForm()
}

func (d *Dashboard) collectProviderFromForm() config.Provider {
	return config.Provider{
		Behavior:         strings.TrimSpace(d.formFields[1].Value()),
		DefaultBaseURL:   strings.TrimSpace(d.formFields[2].Value()),
		DefaultAPIKeyEnv: strings.TrimSpace(d.formFields[3].Value()),
		AnthropicVersion: strings.TrimSpace(d.formFields[4].Value()),
	}
}

func (d *Dashboard) collectMappingFromForm() config.Mapping {
	return config.Mapping{
		ProviderName: strings.TrimSpace(d.formFields[1].Value()),
		ModelString:  strings.TrimSpace(d.formFields[2].Value()),
	}
}
