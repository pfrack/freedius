package tui

import (
	"os"
	"path/filepath"
	"strings"
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
		{name: "press 1", key: tea.KeyPressMsg{Code: '1'}, wantTab: tabLog},
		{name: "press 2", key: tea.KeyPressMsg{Code: '2'}, wantTab: tabProviders},
		{name: "press 3", key: tea.KeyPressMsg{Code: '3'}, wantTab: tabConfig},
		{name: "press q", key: tea.KeyPressMsg{Text: "q"}, wantQuit: true},
		{name: "press ctrl+c", key: tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, wantQuit: true},
		{name: "press esc", key: tea.KeyPressMsg{Code: tea.KeyEsc}, wantQuit: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDashboard(nil, nil, nil, nil, "", "", 0, false)
			// Start on tab 0
			d.activeTab = tabLog

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
			initial: tabLog,
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
			want:    tabLog,
		},
		{
			name:    "shift+tab from 0 wraps to 2",
			key:     tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift},
			initial: tabLog,
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
			d := NewDashboard(nil, nil, nil, nil, "", "", 0, false)
			d.activeTab = tt.initial
			d.Update(tt.key)
			if d.activeTab != tt.want {
				t.Errorf("activeTab = %d, want %d", d.activeTab, tt.want)
			}
		})
	}
}

func TestDashboard_Update_Resize(t *testing.T) {
	d := NewDashboard(nil, nil, nil, nil, "", "", 0, false)
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

	d := NewDashboard(ch, &config.Config{}, nil, nil, "", "", 0, false)

	ev := proxy.RequestEvent{
		RequestID: "test-1",
		Method:    "POST",
		Path:      "/v1/messages",
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
	d := NewDashboard(nil, nil, nil, nil, "", "", 0, false)

	errEv := proxy.RequestEvent{
		RequestID: "err-1",
		Method:    "POST",
		Path:      "/v1/messages",
		Status:    500,
	}
	d.Update(requestEventMsg(errEv))
	if d.stats.errorCount != 1 {
		t.Errorf("errorCount = %d, want 1", d.stats.errorCount)
	}
}

func TestDashboard_OpenEditProviderForm(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
	}
	d := NewDashboard(nil, cfg, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig

	// Cursor 0 = the only provider.
	d.configCursor = 0
	d.Update(tea.KeyPressMsg{Text: "e"})

	if d.formMode != formEditProvider {
		t.Fatalf("formMode = %d, want %d (formEditProvider)", d.formMode, formEditProvider)
	}
	if len(d.formFields) != 5 {
		t.Fatalf("formFields count = %d, want 5", len(d.formFields))
	}
	if d.formFields[0].Value() != "nim" {
		t.Errorf("field 0 (name) = %q, want nim", d.formFields[0].Value())
	}
	if d.formFields[1].Value() != "openai" {
		t.Errorf("field 1 (behavior) = %q, want openai", d.formFields[1].Value())
	}
}

func TestDashboard_OpenEditMappingForm(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"opus": {ProviderName: "nim", ModelString: "meta-llama"},
		},
	}
	d := NewDashboard(nil, cfg, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig

	// collectAllEntries returns providers first, then mappings. With one
	// provider and one mapping, the mapping is at index 1.
	d.configCursor = 1
	d.Update(tea.KeyPressMsg{Text: "e"})

	if d.formMode != formEditMapping {
		t.Fatalf("formMode = %d, want %d (formEditMapping)", d.formMode, formEditMapping)
	}
	if len(d.formFields) != 3 {
		t.Fatalf("formFields count = %d, want 3", len(d.formFields))
	}
	if d.formFields[0].Value() != "opus" {
		t.Errorf("field 0 (name) = %q, want opus", d.formFields[0].Value())
	}
	if d.formFields[1].Value() != "nim" {
		t.Errorf("field 1 (provider) = %q, want nim", d.formFields[1].Value())
	}
	if d.formFields[2].Value() != "meta-llama" {
		t.Errorf("field 2 (model) = %q, want meta-llama", d.formFields[2].Value())
	}
}

func TestDashboard_OpenAddProviderForm(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig

	d.Update(tea.KeyPressMsg{Text: "p"})

	if d.formMode != formAddProvider {
		t.Fatalf("formMode = %d, want %d (formAddProvider)", d.formMode, formAddProvider)
	}
	if len(d.formFields) != 5 {
		t.Fatalf("formFields count = %d, want 5", len(d.formFields))
	}
	for i, f := range d.formFields {
		if f.Value() != "" {
			t.Errorf("field %d should be empty for add form, got %q", i, f.Value())
		}
	}
}

func TestDashboard_OpenAddMappingForm(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig

	d.Update(tea.KeyPressMsg{Text: "a"})

	if d.formMode != formAddMapping {
		t.Fatalf("formMode = %d, want %d (formAddMapping)", d.formMode, formAddMapping)
	}
	if len(d.formFields) != 3 {
		t.Fatalf("formFields count = %d, want 3", len(d.formFields))
	}
	for i, f := range d.formFields {
		if f.Value() != "" {
			t.Errorf("field %d should be empty for add form, got %q", i, f.Value())
		}
	}
}

func TestDashboard_FormCancel(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.configCursor = 0

	// Open edit form on empty config — nothing to edit, form shouldn't open.
	d.Update(tea.KeyPressMsg{Text: "e"})
	if d.formMode != formNone {
		t.Fatal("expected formMode formNone when no entries exist")
	}

	// Add a provider manually and open form.
	d.config.Providers = map[string]config.Provider{"test": {Behavior: "openai"}}
	d.configCursor = 0
	d.Update(tea.KeyPressMsg{Text: "e"})
	if d.formMode != formEditProvider {
		t.Fatalf("formMode = %d, want formEditProvider", d.formMode)
	}

	// Cancel the form.
	d.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if d.formMode != formNone {
		t.Errorf("formMode after esc = %d, want formNone", d.formMode)
	}
}

func TestDashboard_FormFieldNavigation(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.config.Providers = map[string]config.Provider{"test": {Behavior: "openai"}}
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
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.config.Providers = map[string]config.Provider{
		"test": {Behavior: "openai"},
	}
	d.config.Mappings = map[string]config.Mapping{
		"old": {ProviderName: "test", ModelString: "x"},
	}
	d.configCursor = 1 // the mapping

	d.Update(tea.KeyPressMsg{Text: "e"})

	// Clear the model field to make it invalid.
	d.formFields[2].SetValue("")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if d.formMode != formEditMapping {
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

func TestDashboard_FormSubmitProviderInvalidBehavior(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.config.Providers = map[string]config.Provider{
		"test": {Behavior: "openai"},
	}
	d.configCursor = 0
	d.Update(tea.KeyPressMsg{Text: "p"}) // open add provider form

	// Fill the name and an invalid behavior.
	d.formFields[0].SetValue("newprov")
	d.formFields[1].SetValue("garbage")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if d.formMode != formAddProvider {
		t.Fatal("form should stay open after invalid behavior submit")
	}
	errMsg, ok := d.fieldErrors[1]
	if !ok {
		t.Fatalf("expected error on field 1 (behavior), got errors: %v", d.fieldErrors)
	}
	if !strings.Contains(errMsg, "behavior must be one of") {
		t.Errorf("error = %q, want 'behavior must be one of...'", errMsg)
	}
}

func TestDashboard_FormSubmitMappingUnknownProvider(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.config.Providers = map[string]config.Provider{
		"nim": {Behavior: "openai"},
	}

	d.Update(tea.KeyPressMsg{Text: "a"}) // open add mapping form
	d.formFields[0].SetValue("opus")
	d.formFields[1].SetValue("nope") // unknown provider
	d.formFields[2].SetValue("x")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if d.formMode != formAddMapping {
		t.Fatal("form should stay open after invalid provider reference")
	}
	errMsg, ok := d.fieldErrors[1]
	if !ok {
		t.Fatalf("expected error on field 1 (provider), got errors: %v", d.fieldErrors)
	}
	if !strings.Contains(errMsg, "unknown provider") {
		t.Errorf("error = %q, want 'unknown provider...'", errMsg)
	}
}

func TestDashboard_DeleteProvider(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.config.Providers = map[string]config.Provider{
		"test": {Behavior: "openai"},
	}
	d.configCursor = 0

	d.Update(tea.KeyPressMsg{Text: "d"})
	if d.formMode != formDeleteConfirm {
		t.Fatalf("formMode = %d, want formDeleteConfirm", d.formMode)
	}

	d.Update(tea.KeyPressMsg{Text: "y"})

	if d.formMode != formNone {
		t.Errorf("formMode after confirm = %d, want formNone", d.formMode)
	}
	if _, ok := d.config.Providers["test"]; ok {
		t.Error("provider 'test' should have been deleted")
	}
}

func TestDashboard_DeleteMapping(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.config.Providers = map[string]config.Provider{
		"nim": {Behavior: "openai"},
	}
	d.config.Mappings = map[string]config.Mapping{
		"opus": {ProviderName: "nim", ModelString: "x"},
	}
	// Mappings are after providers in collectAllEntries; cursor 1 = the mapping.
	d.configCursor = 1

	d.Update(tea.KeyPressMsg{Text: "d"})
	if d.formMode != formDeleteConfirm {
		t.Fatalf("formMode = %d, want formDeleteConfirm", d.formMode)
	}
	d.Update(tea.KeyPressMsg{Text: "y"})

	if d.formMode != formNone {
		t.Errorf("formMode after confirm = %d, want formNone", d.formMode)
	}
	if _, ok := d.config.Mappings["opus"]; ok {
		t.Error("mapping 'opus' should have been deleted")
	}
}

func TestDashboard_DeleteCancel(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.config.Providers = map[string]config.Provider{
		"test": {Behavior: "openai"},
	}
	d.configCursor = 0

	d.Update(tea.KeyPressMsg{Text: "d"})
	d.Update(tea.KeyPressMsg{Text: "n"})

	if d.formMode != formNone {
		t.Errorf("formMode after cancel = %d, want formNone", d.formMode)
	}
	if _, ok := d.config.Providers["test"]; !ok {
		t.Error("provider 'test' should still exist after cancel")
	}
}

func TestDashboard_SaveConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/freedius.yaml"

	initial := `providers:
  nim:
    behavior: openai
    default_base_url: https://integrate.api.nvidia.com/v1/chat/completions
    default_api_key_env: NVIDIA_NIM_API_KEY
mappings:
  opus:
    provider_name: nim
    model_string: meta/llama-3.1-70b-instruct
`
	if err := os.WriteFile(cfgPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	d := NewDashboard(nil, cfg, nil, nil, cfgPath, "", 0, false)
	d.activeTab = tabConfig
	d.configCursor = 1 // the mapping

	// Open edit form on the mapping and modify the model field.
	d.Update(tea.KeyPressMsg{Text: "e"})
	d.formFields[2].SetValue("meta/llama-4")

	d.fieldErrors = d.validateForm()
	if len(d.fieldErrors) > 0 {
		t.Fatalf("unexpected validation errors: %v", d.fieldErrors)
	}
	d.Update(formSubmittedMsg{})

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "meta/llama-4") {
		t.Errorf("saved file should contain new model name, got:\n%s", string(data))
	}
}

func TestDashboard_AddProviderInsert(t *testing.T) {
	d := NewDashboard(nil, &config.Config{}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig

	// Open add provider form.
	d.Update(tea.KeyPressMsg{Text: "p"})

	// Fill the fields.
	d.formFields[0].SetValue("newprov")
	d.formFields[1].SetValue("openai")
	d.formFields[2].SetValue("https://example.com")
	d.formFields[3].SetValue("EXAMPLE_KEY")

	d.fieldErrors = d.validateForm()
	if len(d.fieldErrors) > 0 {
		t.Fatalf("unexpected validation errors: %v", d.fieldErrors)
	}
	d.Update(formSubmittedMsg{})

	p, ok := d.config.Providers["newprov"]
	if !ok {
		t.Fatal("newprov should exist in Providers after add")
	}
	if p.Behavior != "openai" {
		t.Errorf("behavior = %q, want openai", p.Behavior)
	}
	if p.DefaultBaseURL != "https://example.com" {
		t.Errorf("default_base_url = %q, want https://example.com", p.DefaultBaseURL)
	}
}

func TestDashboard_AddMappingInsert(t *testing.T) {
	d := NewDashboard(nil, &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
	}, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig

	// Open add mapping form.
	d.Update(tea.KeyPressMsg{Text: "a"})

	// Fill the fields.
	d.formFields[0].SetValue("newmap")
	d.formFields[1].SetValue("nim")
	d.formFields[2].SetValue("test-model")

	d.fieldErrors = d.validateForm()
	if len(d.fieldErrors) > 0 {
		t.Fatalf("unexpected validation errors: %v", d.fieldErrors)
	}
	d.Update(formSubmittedMsg{})

	m, ok := d.config.Mappings["newmap"]
	if !ok {
		t.Fatal("newmap should exist in Mappings after add")
	}
	if m.ProviderName != "nim" {
		t.Errorf("provider_name = %q, want nim", m.ProviderName)
	}
	if m.ModelString != "test-model" {
		t.Errorf("model_string = %q, want test-model", m.ModelString)
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

func TestRenderLogTab_Format(t *testing.T) {
	d := NewDashboard(nil, nil, nil, nil, "", "", 0, false)
	d.eventLog.push(proxy.RequestEvent{
		RequestID:       "abc123",
		Method:          "POST",
		Path:            "/v1/messages",
		Model:           "opus",
		Provider:        "nim",
		Status:          200,
		Latency:         42 * time.Millisecond,
		MatchedProvider: "nim",
		MatchedModel:    "step-3.5",
		Timestamp:       time.Date(2026, 1, 1, 15, 4, 5, 0, time.UTC),
	})
	out := stripANSI(renderLogTab(d.eventLog.all(), 80, 24, 0))
	for _, want := range []string{
		"request_id=abc123",
		"method=POST",
		"path=/v1/messages",
		"status=200",
		"duration_ms=42",
		"matched_provider=nim",
		"matched_model=step-3.5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderLogTab output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestRenderLogTab_Empty(t *testing.T) {
	out := stripANSI(renderLogTab(nil, 80, 24, 0))
	if !strings.Contains(out, "No requests yet") {
		t.Errorf("expected empty-state message, got: %s", out)
	}
}

func TestRenderLogTab_ErrorSuffix(t *testing.T) {
	d := NewDashboard(nil, nil, nil, nil, "", "", 0, false)
	d.eventLog.push(proxy.RequestEvent{
		RequestID:    "err-1",
		Method:       "POST",
		Path:         "/v1/messages",
		Status:       500,
		Latency:      100 * time.Millisecond,
		ErrorMessage: "boom",
		Timestamp:    time.Date(2026, 1, 1, 15, 4, 5, 0, time.UTC),
	})
	out := stripANSI(renderLogTab(d.eventLog.all(), 80, 24, 0))
	if !strings.Contains(out, `error="boom"`) {
		t.Errorf("expected error= suffix in output, got: %s", out)
	}
}

func TestRenderTabs_LabelIsLog(t *testing.T) {
	out := stripANSI(renderTabs(0, 80))
	if !strings.Contains(out, "[1] Log") {
		t.Errorf("expected '[1] Log' tab label, got: %s", out)
	}
	if strings.Contains(out, "[1] Requests") {
		t.Errorf("should not contain '[1] Requests' label anymore, got: %s", out)
	}
}

// stripANSI removes ANSI escape codes for test assertions.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestDashboard_CtrlETogglesVerboseErrors(t *testing.T) {
	d := NewDashboard(nil, nil, nil, nil, "", "127.0.0.1", 8082, false)
	if d.verboseErrors {
		t.Fatal("initial verboseErrors should be false")
	}

	d.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if !d.verboseErrors {
		t.Error("expected verboseErrors=true after first Ctrl+E")
	}
	if d.stats.message != "Verbose errors: ON" {
		t.Errorf("expected ON message, got %q", d.stats.message)
	}

	d.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if d.verboseErrors {
		t.Error("expected verboseErrors=false after second Ctrl+E")
	}
	if d.stats.message != "Verbose errors: OFF" {
		t.Errorf("expected OFF message, got %q", d.stats.message)
	}
}

func TestDashboard_CtrlEUpdatesDispatcher(t *testing.T) {
	d := NewDashboard(nil, nil, nil, nil, "", "127.0.0.1", 8082, false)
	d.dispatcher = &proxy.Dispatcher{VerboseErrors: false}

	d.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if !d.dispatcher.VerboseErrors {
		t.Error("dispatcher.VerboseErrors should be true after Ctrl+E")
	}
}

func TestDashboard_CtrlSOutsideConfigNoOp(t *testing.T) {
	d := NewDashboard(nil, nil, nil, nil, "", "127.0.0.1", 8082, false)
	d.activeTab = tabLog
	d.stats.message = "previous"

	d.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if d.stats.message != "previous" {
		t.Errorf("Ctrl+S outside Config tab should not change status message, got %q", d.stats.message)
	}
}

func TestDashboard_CtrlSInConfigInstallsShellRC(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	d := NewDashboard(nil, nil, nil, nil, "", "127.0.0.1", 8082, false)
	d.activeTab = tabConfig
	d.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	if !strings.Contains(d.stats.message, "Shell RC updated") {
		t.Errorf("expected success message, got %q", d.stats.message)
	}
	rcPath := filepath.Join(dir, ".zshrc")
	if _, err := os.Stat(rcPath); err != nil {
		t.Errorf(".zshrc should have been written, got: %v", err)
	}
}

func TestDashboard_CtrlSAlreadyInstalledShowsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	d := NewDashboard(nil, nil, nil, nil, "", "127.0.0.1", 8082, false)
	d.activeTab = tabConfig
	d.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	// Second install should fail (already installed; force=false).
	d.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	if !strings.Contains(d.stats.message, "Shell install failed") {
		t.Errorf("expected failure message on second install, got %q", d.stats.message)
	}
}

func TestDashboard_RequestEventClearsStatusMessage(t *testing.T) {
	d := NewDashboard(nil, nil, nil, nil, "", "127.0.0.1", 8082, false)
	d.stats.message = "Shell RC updated ✓"

	d.Update(requestEventMsg(proxy.RequestEvent{Status: 200}))
	if d.stats.message != "" {
		t.Errorf("status message should clear on next event, got %q", d.stats.message)
	}
}

func viewContent(v tea.View) string {
	return v.Content
}

func TestDashboard_ConfigTabScrollsToCursor(t *testing.T) {
	// Build a config with many entries so the cursor scrolls past the visible
	// window. After moving the cursor down, the rendered window must contain
	// the cursor's entry name.
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim":    {Behavior: "openai"},
			"openai": {Behavior: "openai"},
		},
		Mappings: map[string]config.Mapping{
			"opus":    {ProviderName: "nim", ModelString: "x"},
			"sonnet":  {ProviderName: "nim", ModelString: "x"},
			"haiku":   {ProviderName: "nim", ModelString: "x"},
			"auto":    {ProviderName: "nim", ModelString: "x"},
			"default": {ProviderName: "nim", ModelString: "x"},
		},
	}
	d := NewDashboard(nil, cfg, nil, nil, "", "", 0, false)
	d.activeTab = tabConfig
	d.width = 80
	d.height = 24

	all := collectAllEntries(d.config)
	if len(all) < 5 {
		t.Fatalf("setup: need >= 5 entries, got %d", len(all))
	}
	// Move cursor to the last entry.
	d.configCursor = len(all) - 1
	d.Update(requestEventMsg(proxy.RequestEvent{Status: 200}))

	out := stripANSI(viewContent(d.View()))
	if !strings.Contains(out, all[len(all)-1].name) {
		t.Errorf("last entry %q should be visible after cursor move, got:\n%s",
			all[len(all)-1].name, out)
	}
}

func TestDashboard_ProvidersTabScroll(t *testing.T) {
	// Build a config with many providers so the table overflows the visible
	// window. j/k should scroll the visible window back through history.
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim":       {Behavior: "openai"},
			"openai":    {Behavior: "openai"},
			"anthropic": {Behavior: "anthropic"},
			"zen":       {Behavior: "mix"},
			"go":        {Behavior: "mix"},
			"custom1":   {Behavior: "mix"},
			"custom2":   {Behavior: "mix"},
			"custom3":   {Behavior: "mix"},
			"custom4":   {Behavior: "mix"},
			"custom5":   {Behavior: "mix"},
			"custom6":   {Behavior: "mix"},
			"custom7":   {Behavior: "mix"},
		},
	}
	d := NewDashboard(nil, cfg, nil, nil, "", "", 0, false)
	d.activeTab = tabProviders
	d.width = 80
	// Small height forces overflow: 12 - 4 header lines = 8 visible rows.
	d.height = 12

	// First check the tail view shows the last providers.
	out := stripANSI(viewContent(d.View()))
	if !strings.Contains(out, "custom7") {
		t.Errorf("tail view should show custom7, got:\n%s", out)
	}
	if !strings.Contains(out, "openai") {
		t.Errorf("tail view should show openai (sorted after nim), got:\n%s", out)
	}
	if strings.Contains(out, "anthropic") {
		t.Errorf("tail view should NOT show anthropic, got:\n%s", out)
	}

	// Scroll back (k = up = scroll older). Press k enough times to push
	// custom7 off-screen: with 12 providers and ~5 visible rows, we need
	// at least 7 presses to fully scroll to the top.
	for i := 0; i < 7; i++ {
		d.Update(tea.KeyPressMsg{Text: "k"})
	}
	if d.providerScroll == 0 {
		t.Error("expected providerScroll to increment on k")
	}
	out = stripANSI(viewContent(d.View()))
	if strings.Contains(out, "custom7") {
		t.Errorf("after scroll back, custom7 should not be visible, got:\n%s", out)
	}
	if !strings.Contains(out, "anthropic") {
		t.Errorf("after scroll back to top, anthropic should be visible, got:\n%s", out)
	}

	// Scroll forward (j = down = scroll newer) back to the tail.
	for d.providerScroll > 0 {
		d.Update(tea.KeyPressMsg{Text: "j"})
	}
	out = stripANSI(viewContent(d.View()))
	if !strings.Contains(out, "custom7") {
		t.Errorf("after scrolling forward, custom7 should be visible again, got:\n%s", out)
	}
}

// TestConfig_SaveCreatesParentDir moved to config/config_test.go.
