package tui

type shortcut struct {
	key  string
	desc string
}

var helpShortcuts = []shortcut{
	{"q / Ctrl+C", "Quit"},
	{"?", "Show this help"},
	{"F1 / F2", "Switch to Providers / Config tab"},
	{"Esc", "Back to Log"},
	{"Tab / Shift+Tab", "Cycle tabs (or fields in a form)"},
	{"↑ / k", "Scroll up"},
	{"↓ / j", "Scroll down"},
	{"e / Enter", "Edit config entry (Config tab)"},
	{"a", "Add new mapping (Config tab)"},
	{"p", "Add new provider (Config tab)"},
	{"d", "Delete entry under cursor (Config tab)"},
	{"Ctrl+E", "Toggle verbose errors"},
	{"L", "Cycle log level filter"},
	{"Ctrl+T", "Cycle color theme"},
	{"Ctrl+S", "Install shell RC (Config tab)"},
	{"Tab", "Next form field"},
	{"Shift+Tab", "Previous form field"},
	{"Enter", "Save / open picker"},
	{"Esc", "Cancel form"},
	{"y / n", "Confirm / cancel delete"},
	{"Enter / Esc", "Select / cancel (picker)"},
	{"", ""},
	{"Mouse", ""},
	{"Scroll wheel", "Scroll content"},
	{"Click entry", "Edit config entry (Config tab)"},
	{"Click modal", "Close help"},
}
