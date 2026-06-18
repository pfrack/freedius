// Package tui implements the Bubble Tea terminal dashboard for freedius.
// It provides live request monitoring, provider health display, and
// read-only config inspection through three tabbed views.
package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

type requestEventMsg proxy.RequestEvent

type statsData struct {
	startTime     time.Time
	totalRequests int
	errorCount    int
}

type ringBuffer struct {
	buf  []proxy.RequestEvent
	head int
	size int
	cap  int
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{
		buf: make([]proxy.RequestEvent, capacity),
		cap: capacity,
	}
}

func (rb *ringBuffer) push(e proxy.RequestEvent) {
	rb.buf[rb.head] = e
	rb.head = (rb.head + 1) % rb.cap
	if rb.size < rb.cap {
		rb.size++
	}
}

func (rb *ringBuffer) all() []proxy.RequestEvent {
	if rb.size == 0 {
		return nil
	}
	result := make([]proxy.RequestEvent, rb.size)
	start := (rb.head - rb.size + rb.cap) % rb.cap
	for i := 0; i < rb.size; i++ {
		result[i] = rb.buf[(start+i)%rb.cap]
	}
	return result
}

// Dashboard is the top-level Bubble Tea model for the freedius TUI.
// It owns the event subscription channel, ring buffer, tabs, and stats.
type Dashboard struct {
	activeTab int
	events    <-chan proxy.RequestEvent
	eventLog  *ringBuffer
	config    *config.Config
	registry  *proxy.Registry
	stats     statsData
	width     int
	height    int
	quitting  bool
}

// NewDashboard creates a new Dashboard model subscribed to the given event
// channel, configuration, and adapter registry.
func NewDashboard(
	events <-chan proxy.RequestEvent,
	cfg *config.Config,
	reg *proxy.Registry,
) *Dashboard {
	return &Dashboard{
		activeTab: tabRequests,
		events:    events,
		eventLog:  newRingBuffer(1000),
		config:    cfg,
		registry:  reg,
		stats: statsData{
			startTime: time.Now(),
		},
	}
}

// Init returns the initial command that starts listening for proxy events.
func (d *Dashboard) Init() tea.Cmd {
	return waitForEvent(d.events)
}

// Update handles incoming messages: key presses for tab switching and quit,
// window resize events, and request events from the proxy event bus.
func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			d.quitting = true
			return d, tea.Quit
		case "1":
			d.activeTab = tabRequests
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
		}
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil
	case requestEventMsg:
		ev := proxy.RequestEvent(msg)
		d.eventLog.push(ev)
		d.stats.totalRequests++
		if ev.Status >= 400 {
			d.stats.errorCount++
		}
		return d, waitForEvent(d.events)
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
	bodyHeight := height - 3

	var content string
	switch d.activeTab {
	case tabRequests:
		content = renderRequestsTab(d.eventLog.all(), width, bodyHeight)
	case tabProviders:
		content = renderProvidersTab(d.config, width)
	case tabConfig:
		content = renderConfigTab(d.config, width)
	default:
		content = fmt.Sprintf("Unknown tab: %d", d.activeTab)
	}

	tabs := renderTabs(d.activeTab, width)
	stats := renderStatsBar(d.stats, width)
	body := windowStyle.Width(max(width-2, 0)).Render(content)

	result := fmt.Sprintf("%s\n%s\n%s", tabs, body, stats)
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
