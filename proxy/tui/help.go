package tui

type shortcut struct {
	key  string
	desc string
}

var helpShortcuts = []shortcut{
	{"q / Ctrl+C", "Quit"},
	{"?", "Show this help"},
	{"F1 / F2", "Switch to Providers / Mappings tab"},
	{"Esc", "Back to Log"},
	{"Tab / Shift+Tab", "Cycle tabs (or fields in a form)"},
	{"↑ / k", "Scroll up"},
	{"↓ / j", "Scroll down"},
	{"e / Enter", "Edit entry (Providers / Mappings tab)"},
	{"a", "Add new mapping (Mappings tab)"},
	{"p", "Add new provider (Providers tab)"},
	{"d", "Delete entry under cursor"},
	{"Ctrl+E", "Toggle verbose errors"},
	{"L", "Cycle log level filter"},
	{"Ctrl+T", "Cycle color theme"},
	{"Ctrl+S", "Install shell RC (Mappings tab)"},
	{"Tab", "Next form field"},
	{"Shift+Tab", "Previous form field"},
	{"Enter", "Save / open picker"},
	{"Esc", "Cancel form"},
	{"y / n", "Confirm / cancel delete"},
	{"Enter / Esc", "Select / cancel (picker)"},
	{"", ""},
	{"Mouse", ""},
	{"Scroll wheel", "Scroll content"},
	{"Click entry", "Click entry to edit"},
	{"Click modal", "Close help"},
}
