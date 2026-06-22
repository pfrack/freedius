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

	formMode          int
	formFields        []textinput.Model
	formFocus         int
	formKind          string
	formEntryName     string
	fieldErrors       map[int]string
	showPicker        bool
	picker            *providerPicker
	showHelp          bool
	showProviderModal bool
	cfgPath           string
	providerCursor    int
	mappingsCursor    int
	logScroll         int
	formError         string

	styles       Styles
	isDark       bool
	currentTheme *Theme

	detachOnQuit bool
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

// NewAttachDashboard creates a Dashboard for IPC attach mode. It accepts nil
// reg/dispatcher (the attach client only observes via SSE). detachOnQuit is
// set to true so pressing 'q' detaches without killing the daemon.
func NewAttachDashboard(
	events <-chan proxy.RequestEvent,
	logs <-chan proxy.LogEntry,
	cfgPath string,
	host string,
	port int,
) *Dashboard {
	isDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	theme := resolveTheme("")
	return &Dashboard{
		activeTab:       tabLog,
		events:          events,
		logs:            logs,
		logBuffer:       newRingBuffer[proxy.LogEntry](1000),
		currentLogLevel: filterAll,
		cfgPath:         cfgPath,
		host:            host,
		port:            port,
		stats: statsData{
			startTime: time.Now(),
		},
		isDark:       isDark,
		currentTheme: theme,
		styles:       NewStyles(theme.Palette, isDark),
		detachOnQuit: true,
	}
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
			if fieldName == "provider" || fieldName == "behavior" || fieldName == "protocol" {
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

		// --- Provider modal key handling ---
		if d.showProviderModal {
			if d.showPicker {
				return d.handleFormKeyPress(msg)
			}
			switch msg.String() {
			case "esc":
				d.closeProviderModal()
				return d, nil
			case "tab", "shift+tab", "enter":
				return d.handleFormKeyPress(msg)
			default:
				return d.handleFormKeyPress(msg)
			}
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

	// --- Mouse events ---
	case tea.MouseClickMsg:
		return d.handleMouseClick(msg)
	case tea.MouseWheelMsg:
		return d.handleMouseWheel(msg)

	// --- Window resize ---
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil

	// --- TUI resume after suspend ---
	case tea.ResumeMsg:
		d.stats.message = ""
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

func (d *Dashboard) handleTabModeKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { //nolint:gocyclo
	switch msg.String() {
	case "?":
		d.showHelp = true
		return d, nil
	case "q", "ctrl+c":
		d.quitting = true
		return d, tea.Quit
	case "ctrl+z":
		return d, tea.Suspend
	case "f1":
		d.activeTab = tabProviders
		return d, nil
	case "f2":
		d.activeTab = tabMappings
		return d, nil
	case "esc":
		if d.activeTab != tabLog {
			d.activeTab = tabLog
		}
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
		switch d.activeTab {
		case tabProviders:
			d.openEditProviderFormModal()
		case tabMappings:
			d.openEditMappingForm()
		}
		return d, nil
	case "a":
		if d.activeTab == tabMappings {
			d.openAddMappingForm()
		}
		return d, nil
	case "p":
		if d.activeTab == tabProviders {
			d.openAddProviderFormModal()
		}
		return d, nil
	case "d":
		switch d.activeTab {
		case tabProviders:
			providers := collectProvidersFromConfig(d.config)
			if d.providerCursor >= 0 && d.providerCursor < len(providers) {
				d.formEntryName = providers[d.providerCursor].name
				d.formKind = "provider"
				d.formMode = formDeleteConfirm
			}
		case tabMappings:
			all := collectMappingEntries(d.config)
			if d.mappingsCursor >= 0 && d.mappingsCursor < len(all) {
				entry := all[d.mappingsCursor]
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
		if d.activeTab != tabMappings {
			return d, nil
		}
		d.installShellRC()
		return d, nil
	case "ctrl+t":
		d.cycleTheme()
		return d, nil
	}
	return d, nil
}

// scrollUp moves the active tab's cursor / scroll window one step toward
// older entries. For tabs without scroll state (Log uses scroll, Providers
// uses scroll, Config uses cursor) the per-tab helper applies.
func (d *Dashboard) scrollUp() {
	switch d.activeTab {
	case tabMappings:
		d.mappingsCursor--
		if d.mappingsCursor < 0 {
			d.mappingsCursor = 0
		}
	case tabProviders:
		d.providerCursor--
		if d.providerCursor < 0 {
			d.providerCursor = 0
		}
	case tabLog:
		d.logScroll++
	}
}

// scrollDown moves the active tab's cursor / scroll window one step toward
// newer entries.
func (d *Dashboard) scrollDown() {
	switch d.activeTab {
	case tabMappings:
		all := collectMappingEntries(d.config)
		d.mappingsCursor++
		if d.mappingsCursor >= len(all) {
			d.mappingsCursor = len(all) - 1
		}
	case tabProviders:
		providers := collectProvidersFromConfig(d.config)
		d.providerCursor++
		if d.providerCursor >= len(providers) {
			d.providerCursor = len(providers) - 1
		}
	case tabLog:
		if d.logScroll > 0 {
			d.logScroll--
		}
	}
}

func (d *Dashboard) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if d.showHelp || d.showProviderModal || d.formMode != formNone {
		return d, nil
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		d.scrollUp()
	case tea.MouseWheelDown:
		d.scrollDown()
	}
	return d, nil
}

func (d *Dashboard) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return d, nil
	}
	if d.showHelp {
		d.showHelp = false
		return d, nil
	}
	if d.showProviderModal || d.formMode != formNone {
		return d, nil
	}
	if msg.Y == 0 {
		return d, nil
	}
	switch d.activeTab {
	case tabProviders:
		d.handleProvidersClick(msg.Y)
	case tabMappings:
		d.handleMappingsClick(msg.Y)
	}
	return d, nil
}

func (d *Dashboard) handleProvidersClick(y int) {
	providers := collectProvidersFromConfig(d.config)
	if len(providers) == 0 {
		return
	}

	bodyOffset := y - 1
	entryOffset := bodyOffset - 2
	if entryOffset < 0 {
		return
	}

	available := d.height - 1 - 3
	visible := available
	if visible > len(providers) {
		visible = len(providers)
	}
	half := visible / 2
	start := d.providerCursor - half
	if start < 0 {
		start = 0
	}
	idx := entryOffset/1 + start
	if idx >= 0 && idx < len(providers) {
		d.providerCursor = idx
		d.openEditProviderForm()
	}
}

func (d *Dashboard) handleMappingsClick(y int) {
	all := collectMappingEntries(d.config)
	if len(all) == 0 {
		return
	}

	bodyOffset := y - 1
	entryOffset := bodyOffset - 2
	if entryOffset < 0 {
		return
	}

	start, _ := configVisibleWindow(all, d.mappingsCursor, d.height-1-3, 4)
	idx := entryOffset/4 + start
	if idx >= 0 && idx < len(all) {
		d.mappingsCursor = idx
		d.openEditMappingForm()
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

// cycleTheme advances to the next theme in the registry and rebuilds styles.
func (d *Dashboard) cycleTheme() {
	for i, t := range themeRegistry {
		if t.Name == d.currentTheme.Name {
			next := (i + 1) % len(themeRegistry)
			d.currentTheme = &themeRegistry[next]
			d.styles = NewStyles(d.currentTheme.Palette, d.isDark)
			d.stats.message = fmt.Sprintf("Theme: %s", d.currentTheme.Name)
			return
		}
	}
	d.currentTheme = &themeRegistry[0]
	d.styles = NewStyles(d.currentTheme.Palette, d.isDark)
	d.stats.message = fmt.Sprintf("Theme: %s", d.currentTheme.Name)
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
				if fieldName == "protocol" &&
					(d.formMode == formAddProvider || d.formMode == formEditProvider) {
					d.picker = newProtocolPicker(d.styles)
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
	// Reserve 1 row for the topbar (stats + tab indicators).
	bodyHeight := height - 1

	var content string
	if d.formMode != formNone && !d.showProviderModal {
		content = renderForm(d, width, bodyHeight)
	} else {
		d.styleBody = d.activeTab != tabLog
		switch d.activeTab {
		case tabLog:
			content = renderLogTab(d.logBuffer.all(), width, bodyHeight, d.logScroll, d.currentLogLevel, d.styles)
		case tabProviders:
			content = renderProvidersTab(d.config, d.providerCursor, width, bodyHeight, d.styles)
		case tabMappings:
			content = renderMappingsTab(d.config, d.mappingsCursor, width, bodyHeight, d.styles)
		default:
			content = fmt.Sprintf("Unknown tab: %d", d.activeTab)
		}
	}

	stats := renderStatsBar(d.stats, width, d.styles)
	var body string
	if d.styleBody {
		body = d.styles.WindowStyle.Width(max(width-2, 0)).Render(content)
	} else {
		body = content
	}

	result := fmt.Sprintf("%s\n%s", stats, body)

	if d.showProviderModal {
		modal := renderProviderEditModal(width, d)
		result = overlayModal(result, modal, width, height, d.styles.OverlayBgStyle)
	}

	if d.showHelp {
		modal := renderHelpModal(width, d.styles)
		result = overlayModal(result, modal, width, height, d.styles.OverlayBgStyle)
	}

	v := tea.NewView(result)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
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

func (d *Dashboard) openEditProviderForm() {
	if d.detachOnQuit {
		return
	}
	providers := collectProvidersFromConfig(d.config)
	if d.providerCursor < 0 || d.providerCursor >= len(providers) {
		return
	}
	p := providers[d.providerCursor]
	d.formKind = "provider"
	d.formEntryName = p.name

	cfgP := d.config.ProvidersSnapshot()[p.name]
	d.formFields = []textinput.Model{
		d.newFormField(p.name, "provider name"),
		d.newFormField(p.behavior, "behavior"),
		d.newFormField(p.baseURL, "base_url"),
		d.newFormField(cfgP.DefaultAPIKeyEnv, "api_key_env"),
		d.newFormField(cfgP.AnthropicVersion, "anthropic_version"),
		d.newFormField(cfgP.Protocol, "protocol (openai/anthropic)"),
	}
	d.formMode = formEditProvider
	d.formFocus = 0
	d.updateFormFocus()
	d.fieldErrors = nil
	d.formError = ""
	d.showPicker = false
}

func (d *Dashboard) openEditMappingForm() {
	if d.detachOnQuit {
		return
	}
	all := collectMappingEntries(d.config)
	if d.mappingsCursor < 0 || d.mappingsCursor >= len(all) {
		return
	}
	entry := all[d.mappingsCursor]
	d.formKind = entry.kind
	d.formEntryName = entry.name

	m := entry.mapping
	d.formFields = []textinput.Model{
		d.newFormField(entry.name, "mapping name"),
		d.newFormField(m.ProviderName, "provider"),
		d.newFormField(m.ModelString, "model"),
	}
	d.formMode = formEditMapping
	d.formFocus = 0
	d.updateFormFocus()
	d.fieldErrors = nil
	d.formError = ""
	d.showPicker = false
}

func (d *Dashboard) openAddProviderForm() {
	if d.detachOnQuit {
		return
	}
	d.formKind = "provider"
	d.formEntryName = ""

	d.formFields = []textinput.Model{
		d.newFormField("", "provider name"),
		d.newFormField("", "behavior"),
		d.newFormField("", "base_url"),
		d.newFormField("", "api_key_env"),
		d.newFormField("", "anthropic_version"),
		d.newFormField("", "protocol (openai/anthropic)"),
	}
	d.formFocus = 0
	d.updateFormFocus()
	d.fieldErrors = nil
	d.formError = ""
	d.showPicker = false
	d.formMode = formAddProvider
}

func (d *Dashboard) openAddMappingForm() {
	if d.detachOnQuit {
		return
	}
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

func (d *Dashboard) openEditProviderFormModal() {
	if d.detachOnQuit {
		return
	}
	d.openEditProviderForm()
	if d.formMode == formEditProvider {
		d.showProviderModal = true
	}
}

func (d *Dashboard) openAddProviderFormModal() {
	if d.detachOnQuit {
		return
	}
	d.openAddProviderForm()
	d.showProviderModal = true
}

func (d *Dashboard) closeProviderModal() {
	d.resetForm()
	d.showProviderModal = false
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
	d.showProviderModal = false
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
		protocol := strings.TrimSpace(d.formFields[5].Value())
		switch protocol {
		case "", "openai", "anthropic":
			// valid
		default:
			errs[5] = "protocol must be one of: openai, anthropic, or empty"
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
		Protocol:         strings.TrimSpace(d.formFields[5].Value()),
	}
}

func (d *Dashboard) collectMappingFromForm() config.Mapping {
	return config.Mapping{
		ProviderName: strings.TrimSpace(d.formFields[1].Value()),
		ModelString:  strings.TrimSpace(d.formFields[2].Value()),
	}
}
