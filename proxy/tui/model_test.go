package tui

import (
	"os"
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

func TestDashboard_OpenEditProviderForm(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"nim": {Behavior: "openai"},
		},
	}
	d := NewDashboard(nil, cfg, nil, "")
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
	d := NewDashboard(nil, cfg, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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

	d := NewDashboard(nil, cfg, nil, cfgPath)
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
	d := NewDashboard(nil, &config.Config{}, nil, "")
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
	}, nil, "")
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