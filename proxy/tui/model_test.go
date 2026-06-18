package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

func init() {
	// Ensure there's at least one known provider for form tests.
	if _, ok := config.KnownProviders["nim"]; !ok {
		panic("nim provider not in KnownProviders")
	}
}

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
			d := NewDashboard(nil, nil, nil, "")
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
			d := NewDashboard(nil, nil, nil, "")
			d.activeTab = tt.initial
			d.Update(tt.key)
			if d.activeTab != tt.want {
				t.Errorf("activeTab = %d, want %d", d.activeTab, tt.want)
			}
		})
	}
}

func TestDashboard_Update_Resize(t *testing.T) {
	d := NewDashboard(nil, nil, nil, "")
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

	d := NewDashboard(ch, &config.Config{}, nil, "")

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
	d := NewDashboard(nil, nil, nil, "")

	errEv := proxy.RequestEvent{
		RequestID: "err-1",
		Status:    500,
	}
	d.Update(requestEventMsg(errEv))
	if d.stats.errorCount != 1 {
		t.Errorf("errorCount = %d, want 1", d.stats.errorCount)
	}
}

func TestDashboard_OpenEditForm(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{
			"opus": {Provider: "nim", Model: "meta/llama-3.1-70b-instruct"},
		},
	}
	d := NewDashboard(nil, cfg, nil, "")
	d.activeTab = tabConfig

	// Select the first entry and press e.
	d.configCursor = 0
	d.Update(tea.KeyPressMsg{Text: "e"})

	if d.formMode != formEdit {
		t.Fatalf("formMode = %d, want %d (formEdit)", d.formMode, formEdit)
	}
	if len(d.formFields) != 7 {
		t.Fatalf("formFields count = %d, want 7", len(d.formFields))
	}
	if d.formFields[0].Value() != "opus" {
		t.Errorf("field 0 (name) = %q, want opus", d.formFields[0].Value())
	}
	if d.formFields[1].Value() != "nim" {
		t.Errorf("field 1 (provider) = %q, want nim", d.formFields[1].Value())
	}
	if d.formFields[2].Value() != "meta/llama-3.1-70b-instruct" {
		t.Errorf("field 2 (model) = %q, want meta/llama-3.1-70b-instruct", d.formFields[2].Value())
	}
}

func TestDashboard_OpenAddForm(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, "")
	d.activeTab = tabConfig

	d.Update(tea.KeyPressMsg{Text: "a"})

	if d.formMode != formAdd {
		t.Fatalf("formMode = %d, want %d (formAdd)", d.formMode, formAdd)
	}
	if len(d.formFields) != 7 {
		t.Fatalf("formFields count = %d, want 7", len(d.formFields))
	}
	// All fields should be empty for add.
	for i, f := range d.formFields {
		if f.Value() != "" {
			t.Errorf("field %d should be empty for add form, got %q", i, f.Value())
		}
	}
}

func TestDashboard_FormCancel(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, "")
	d.activeTab = tabConfig
	d.configCursor = 0

	// Open edit form on empty config — nothing to edit, form shouldn't open.
	d.Update(tea.KeyPressMsg{Text: "e"})
	if d.formMode != formNone {
		t.Fatal("expected formMode formNone when no entries exist")
	}

	// Add a model manually and open form.
	d.config.Models = map[string]config.Model{"test": {Provider: "nim", Model: "test-model"}}
	d.configCursor = 0
	d.Update(tea.KeyPressMsg{Text: "e"})
	if d.formMode != formEdit {
		t.Fatalf("formMode = %d, want formEdit", d.formMode)
	}

	// Cancel the form.
	d.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if d.formMode != formNone {
		t.Errorf("formMode after esc = %d, want formNone", d.formMode)
	}
}

func TestDashboard_FormFieldNavigation(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, "")
	d.activeTab = tabConfig
	d.config.Models = map[string]config.Model{"test": {Provider: "nim", Model: "test-model"}}
	d.configCursor = 0

	d.Update(tea.KeyPressMsg{Text: "e"})
	if d.formFocus != 0 {
		t.Errorf("initial focus = %d, want 0", d.formFocus)
	}

	// Tab to next field.
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if d.formFocus != 1 {
		t.Errorf("after tab, focus = %d, want 1", d.formFocus)
	}

	// Shift+Tab back to previous field.
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if d.formFocus != 0 {
		t.Errorf("after shift+tab, focus = %d, want 0", d.formFocus)
	}
}

func TestDashboard_FormSubmitInvalidShowsErrors(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, "")
	d.activeTab = tabConfig
	d.config.Models = map[string]config.Model{"test": {Provider: "nim", Model: "test-model"}}
	d.configCursor = 0

	d.Update(tea.KeyPressMsg{Text: "e"})

	// Clear the model field to make it invalid.
	d.formFields[2].SetValue("")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if d.formMode != formEdit {
		t.Fatal("expected form to stay open after invalid submit")
	}
	if len(d.fieldErrors) == 0 {
		t.Error("expected field errors after invalid submit")
	}
	errMsg, ok := d.fieldErrors[2]
	if !ok {
		t.Errorf("expected error on field 2 (model), got errors: %v", d.fieldErrors)
	}
	if errMsg != "model is required" {
		t.Errorf("error message = %q, want 'model is required'", errMsg)
	}
}

func TestDashboard_DeleteConfirm(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, "")
	d.activeTab = tabConfig
	d.config.Models = map[string]config.Model{"test": {Provider: "nim", Model: "test-model"}}
	d.configCursor = 0

	d.Update(tea.KeyPressMsg{Text: "d"})
	if d.formMode != formDeleteConfirm {
		t.Fatalf("formMode = %d, want formDeleteConfirm", d.formMode)
	}

	// Confirm deletion.
	d.Update(tea.KeyPressMsg{Text: "y"})

	if d.formMode != formNone {
		t.Errorf("formMode after confirm = %d, want formNone", d.formMode)
	}
	if _, ok := d.config.Models["test"]; ok {
		t.Error("model 'test' should have been deleted")
	}
}

func TestDashboard_DeleteCancel(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, "")
	d.activeTab = tabConfig
	d.config.Models = map[string]config.Model{"test": {Provider: "nim", Model: "test-model"}}
	d.configCursor = 0

	d.Update(tea.KeyPressMsg{Text: "d"})
	d.Update(tea.KeyPressMsg{Text: "n"})

	if d.formMode != formNone {
		t.Errorf("formMode after cancel = %d, want formNone", d.formMode)
	}
	if _, ok := d.config.Models["test"]; !ok {
		t.Error("model 'test' should still exist after cancel")
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
