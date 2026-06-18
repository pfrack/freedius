package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

func TestDashboard_Update_KeyPress(t *testing.T) {
	tests := []struct {
		name     string
		key      tea.KeyPressMsg
		wantTab  int
		wantQuit bool
	}{
		{name: "press 1", key: tea.KeyPressMsg{Code: '1'}, wantTab: tabRequests},
		{name: "press 2", key: tea.KeyPressMsg{Code: '2'}, wantTab: tabProviders},
		{name: "press 3", key: tea.KeyPressMsg{Code: '3'}, wantTab: tabConfig},
		{name: "press q", key: tea.KeyPressMsg{Text: "q"}, wantQuit: true},
		{name: "press ctrl+c", key: tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, wantQuit: true},
		{name: "press esc", key: tea.KeyPressMsg{Code: tea.KeyEsc}, wantQuit: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDashboard(nil, nil, nil)
			// Start on tab 0
			d.activeTab = tabRequests

			_, cmd := d.Update(tt.key)
			if tt.wantQuit {
				if !d.quitting {
					t.Error("expected quitting to be true")
				}
				if cmd == nil {
					t.Error("expected quit command")
				}
			} else if d.activeTab != tt.wantTab {
				t.Errorf("activeTab = %d, want %d", d.activeTab, tt.wantTab)
			}
		})
	}
}

func TestDashboard_Update_TabCycle(t *testing.T) {
	tests := []struct {
		name    string
		key     tea.KeyPressMsg
		initial int
		want    int
	}{
		{
			name:    "tab from 0 to 1",
			key:     tea.KeyPressMsg{Code: tea.KeyTab},
			initial: tabRequests,
			want:    tabProviders,
		},
		{
			name:    "tab from 1 to 2",
			key:     tea.KeyPressMsg{Code: tea.KeyTab},
			initial: tabProviders,
			want:    tabConfig,
		},
		{
			name:    "tab from 2 wraps to 0",
			key:     tea.KeyPressMsg{Code: tea.KeyTab},
			initial: tabConfig,
			want:    tabRequests,
		},
		{
			name:    "shift+tab from 0 wraps to 2",
			key:     tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift},
			initial: tabRequests,
			want:    tabConfig,
		},
		{
			name:    "shift+tab from 2 to 1",
			key:     tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift},
			initial: tabConfig,
			want:    tabProviders,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDashboard(nil, nil, nil)
			d.activeTab = tt.initial
			d.Update(tt.key)
			if d.activeTab != tt.want {
				t.Errorf("activeTab = %d, want %d", d.activeTab, tt.want)
			}
		})
	}
}

func TestDashboard_Update_Resize(t *testing.T) {
	d := NewDashboard(nil, nil, nil)
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if d.width != 120 {
		t.Errorf("width = %d, want 120", d.width)
	}
	if d.height != 40 {
		t.Errorf("height = %d, want 40", d.height)
	}
}

func TestDashboard_Update_EventMsg(t *testing.T) {
	ch := make(chan proxy.RequestEvent, 1)
	defer close(ch)

	d := NewDashboard(ch, &config.Config{}, nil)

	ev := proxy.RequestEvent{
		RequestID: "test-1",
		Model:     "opus",
		Provider:  "nim",
		Status:    200,
		Latency:   10 * time.Millisecond,
	}
	ch <- ev

	_, cmd := d.Update(requestEventMsg(ev))
	if d.stats.totalRequests != 1 {
		t.Errorf("totalRequests = %d, want 1", d.stats.totalRequests)
	}
	if d.stats.errorCount != 0 {
		t.Errorf("errorCount = %d, want 0 (status 200)", d.stats.errorCount)
	}
	events := d.eventLog.all()
	if len(events) != 1 {
		t.Fatalf("eventLog size = %d, want 1", len(events))
	}
	if events[0].RequestID != "test-1" {
		t.Errorf("event RequestID = %q, want test-1", events[0].RequestID)
	}
	if cmd == nil {
		t.Error("expected re-arm command after event")
	}
}

func TestDashboard_Update_EventMsgErrorCount(t *testing.T) {
	d := NewDashboard(nil, nil, nil)

	errEv := proxy.RequestEvent{
		RequestID: "err-1",
		Status:    500,
	}
	d.Update(requestEventMsg(errEv))
	if d.stats.errorCount != 1 {
		t.Errorf("errorCount = %d, want 1", d.stats.errorCount)
	}
}

func TestRingBuffer(t *testing.T) {
	rb := newRingBuffer(3)

	if all := rb.all(); len(all) != 0 {
		t.Errorf("empty ring buffer should return 0 events, got %d", len(all))
	}

	rb.push(proxy.RequestEvent{RequestID: "a"})
	rb.push(proxy.RequestEvent{RequestID: "b"})
	rb.push(proxy.RequestEvent{RequestID: "c"})

	all := rb.all()
	if len(all) != 3 {
		t.Fatalf("size = %d, want 3", len(all))
	}
	for i, want := range []string{"a", "b", "c"} {
		if all[i].RequestID != want {
			t.Errorf("event[%d] RequestID = %q, want %q", i, all[i].RequestID, want)
		}
	}

	// Overflow: push beyond capacity.
	rb.push(proxy.RequestEvent{RequestID: "d"})
	all = rb.all()
	if len(all) != 3 {
		t.Fatalf("size after overflow = %d, want 3", len(all))
	}
	for i, want := range []string{"b", "c", "d"} {
		if all[i].RequestID != want {
			t.Errorf("event[%d] RequestID = %q, want %q", i, all[i].RequestID, want)
		}
	}
}
