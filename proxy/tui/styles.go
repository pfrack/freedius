package tui

import (
	"fmt"
	"image/color"

	lipgloss "charm.land/lipgloss/v2"
)

// AdaptiveColor holds light and dark variants for a single color slot.
// The active variant is selected at runtime via lipgloss.LightDark based on
// the terminal background.
type AdaptiveColor struct {
	Light color.Color
	Dark  color.Color
}

// Palette defines the seven named color slots that theme implementations
// provide. Each slot carries light/dark adaptive variants.
type Palette struct {
	Error      AdaptiveColor
	Warning    AdaptiveColor
	Accent     AdaptiveColor
	KeyCap     AdaptiveColor
	Muted      AdaptiveColor
	Border     AdaptiveColor
	Background AdaptiveColor
}

// Theme pairs a human-readable label with a full color Palette.
type Theme struct {
	Label   string
	Palette Palette
}

// Styles bundles all pre-built lipgloss.Style values used by TUI render
// functions. Created via NewStyles from a Palette.
type Styles struct {
	ActiveTabStyle           lipgloss.Style
	InactiveTabStyle         lipgloss.Style
	StatusClientErrStyle     lipgloss.Style
	StatusErrorStyle         lipgloss.Style
	StatsBarStyle            lipgloss.Style
	StatsBarPartStyle        lipgloss.Style
	TabBarStyle              lipgloss.Style
	WindowStyle              lipgloss.Style
	ProviderTableHeaderStyle lipgloss.Style
	ConfigKeyStyle           lipgloss.Style
	ConfigValueStyle         lipgloss.Style
	SeparatorStyle           lipgloss.Style
	ModalStyle               lipgloss.Style
	ModalTitleStyle          lipgloss.Style
	ModalFooterStyle         lipgloss.Style
	ShortcutKeyStyle         lipgloss.Style
	ShortcutDescStyle        lipgloss.Style
	OverlayBgStyle           lipgloss.Style
	LogInfoStyle             lipgloss.Style
	LogDebugStyle            lipgloss.Style
}

// DefaultPalette returns the palette that reproduces the original hard-coded
// ANSI 8-color styling. Dark variants match the existing ANSI codes; light
// variants use bright ANSI equivalents for visibility on light terminals.
func DefaultPalette() Palette {
	return Palette{
		Error:      AdaptiveColor{Light: lipgloss.Color("9"), Dark: lipgloss.Color("1")},
		Warning:    AdaptiveColor{Light: lipgloss.Color("11"), Dark: lipgloss.Color("3")},
		Accent:     AdaptiveColor{Light: lipgloss.Color("12"), Dark: lipgloss.Color("4")},
		KeyCap:     AdaptiveColor{Light: lipgloss.Color("14"), Dark: lipgloss.Color("6")},
		Muted:      AdaptiveColor{Light: lipgloss.Color("15"), Dark: lipgloss.Color("7")},
		Border:     AdaptiveColor{Light: lipgloss.Color("8"), Dark: lipgloss.Color("8")},
		Background: AdaptiveColor{Light: lipgloss.Color("15"), Dark: lipgloss.Color("0")},
	}
}

// DefaultTheme returns the "default" theme entry wrapping DefaultPalette.
func DefaultTheme() Theme {
	return Theme{Label: "default", Palette: DefaultPalette()}
}

// themeRegistry is the O(1)-lookup map of available themes. Immutable after
// init. themeOrder is the cycling order; must stay in sync with
// themeRegistry keys. New themes must be added to BOTH.
var (
	themeRegistry = map[string]Theme{
		"default": DefaultTheme(),
		"zenburn": {
			Label: "zenburn",
			Palette: Palette{
				Error:      AdaptiveColor{Light: lipgloss.Color("#cc9393"), Dark: lipgloss.Color("#cc9393")},
				Warning:    AdaptiveColor{Light: lipgloss.Color("#e0c989"), Dark: lipgloss.Color("#e0c989")},
				Accent:     AdaptiveColor{Light: lipgloss.Color("#8cd0d3"), Dark: lipgloss.Color("#8cd0d3")},
				KeyCap:     AdaptiveColor{Light: lipgloss.Color("#f0dfaf"), Dark: lipgloss.Color("#f0dfaf")},
				Muted:      AdaptiveColor{Light: lipgloss.Color("#dcdccc"), Dark: lipgloss.Color("#dcdccc")},
				Border:     AdaptiveColor{Light: lipgloss.Color("#7f9f7f"), Dark: lipgloss.Color("#7f9f7f")},
				Background: AdaptiveColor{Light: lipgloss.Color("#3f3f3f"), Dark: lipgloss.Color("#3f3f3f")},
			},
		},
		"gruvbox-dark": {
			Label: "gruvbox-dark",
			Palette: Palette{
				Error:      AdaptiveColor{Light: lipgloss.Color("#fb4934"), Dark: lipgloss.Color("#fb4934")},
				Warning:    AdaptiveColor{Light: lipgloss.Color("#fabd2f"), Dark: lipgloss.Color("#fabd2f")},
				Accent:     AdaptiveColor{Light: lipgloss.Color("#83a598"), Dark: lipgloss.Color("#83a598")},
				KeyCap:     AdaptiveColor{Light: lipgloss.Color("#b8bb26"), Dark: lipgloss.Color("#b8bb26")},
				Muted:      AdaptiveColor{Light: lipgloss.Color("#ebdbb2"), Dark: lipgloss.Color("#ebdbb2")},
				Border:     AdaptiveColor{Light: lipgloss.Color("#928374"), Dark: lipgloss.Color("#928374")},
				Background: AdaptiveColor{Light: lipgloss.Color("#282828"), Dark: lipgloss.Color("#282828")},
			},
		},
		"catppuccin-mocha": {
			Label: "catppuccin-mocha",
			Palette: Palette{
				Error:      AdaptiveColor{Light: lipgloss.Color("#f38ba8"), Dark: lipgloss.Color("#f38ba8")},
				Warning:    AdaptiveColor{Light: lipgloss.Color("#fab387"), Dark: lipgloss.Color("#fab387")},
				Accent:     AdaptiveColor{Light: lipgloss.Color("#89b4fa"), Dark: lipgloss.Color("#89b4fa")},
				KeyCap:     AdaptiveColor{Light: lipgloss.Color("#a6e3a1"), Dark: lipgloss.Color("#a6e3a1")},
				Muted:      AdaptiveColor{Light: lipgloss.Color("#cdd6f4"), Dark: lipgloss.Color("#cdd6f4")},
				Border:     AdaptiveColor{Light: lipgloss.Color("#585b70"), Dark: lipgloss.Color("#585b70")},
				Background: AdaptiveColor{Light: lipgloss.Color("#1e1e2e"), Dark: lipgloss.Color("#1e1e2e")},
			},
		},
	}
	themeOrder = [...]string{"default", "zenburn", "gruvbox-dark", "catppuccin-mocha"}
)

// resolveTheme looks up a theme by label in O(1). If not found, it
// returns the default theme (unknown labels silently fall back).
func resolveTheme(label string) *Theme {
	if t, ok := themeRegistry[label]; ok {
		return &t
	}
	t := themeRegistry[themeOrder[0]]
	return &t
}

// NewStyles builds a complete Styles struct from a palette. Each color slot
// is resolved through lipgloss.LightDark(isDark) so the active variant
// matches the terminal background.
func NewStyles(p Palette, isDark bool) Styles {
	if err := validatePalette(p); err != nil {
		panic(fmt.Sprintf("tui: invalid theme palette: %v", err))
	}
	fn := lipgloss.LightDark(isDark)
	return Styles{
		ActiveTabStyle: lipgloss.NewStyle().
			Bold(true).
			Underline(true).
			Padding(0, 1).
			Foreground(fn(p.Accent.Light, p.Accent.Dark)),
		InactiveTabStyle: lipgloss.NewStyle().
			Faint(true).
			Padding(0, 1).
			Foreground(fn(p.Muted.Light, p.Muted.Dark)),
		StatusClientErrStyle: lipgloss.NewStyle().
			Foreground(fn(p.Warning.Light, p.Warning.Dark)),
		StatusErrorStyle: lipgloss.NewStyle().
			Foreground(fn(p.Error.Light, p.Error.Dark)),
		StatsBarStyle: lipgloss.NewStyle().
			Padding(0, 1).
			Background(fn(p.Accent.Light, p.Accent.Dark)),
		StatsBarPartStyle: lipgloss.NewStyle().
			Background(fn(p.Accent.Light, p.Accent.Dark)),
		TabBarStyle: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(fn(p.Border.Light, p.Border.Dark)),
		WindowStyle: lipgloss.NewStyle().
			Padding(0, 1),
		ProviderTableHeaderStyle: lipgloss.NewStyle().
			Bold(true).
			Underline(true).
			Foreground(fn(p.Accent.Light, p.Accent.Dark)),
		ConfigKeyStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(fn(p.Accent.Light, p.Accent.Dark)),
		ConfigValueStyle: lipgloss.NewStyle().
			Faint(true).
			Foreground(fn(p.Muted.Light, p.Muted.Dark)),
		SeparatorStyle: lipgloss.NewStyle().
			Foreground(fn(p.Border.Light, p.Border.Dark)),
		ModalStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(fn(p.Accent.Light, p.Accent.Dark)).
			Padding(1, 2),
		ModalTitleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(fn(p.Accent.Light, p.Accent.Dark)).
			Padding(0, 1),
		ModalFooterStyle: lipgloss.NewStyle().
			Faint(true).
			Italic(true),
		ShortcutKeyStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(fn(p.KeyCap.Light, p.KeyCap.Dark)),
		ShortcutDescStyle: lipgloss.NewStyle().
			Foreground(fn(p.Muted.Light, p.Muted.Dark)),
		OverlayBgStyle: lipgloss.NewStyle().
			Background(fn(p.Background.Light, p.Background.Dark)),
		LogInfoStyle: lipgloss.NewStyle().
			Foreground(fn(p.KeyCap.Light, p.KeyCap.Dark)),
		LogDebugStyle: lipgloss.NewStyle().
			Foreground(fn(p.Muted.Light, p.Muted.Dark)).
			Faint(true),
	}
}

// validatePalette returns an error if any AdaptiveColor has both Light and
// Dark set to nil. This catches incomplete theme definitions at construction
// time rather than producing nil colors that lipgloss would silently ignore.
func validatePalette(p Palette) error {
	if p.Error.Light == nil && p.Error.Dark == nil {
		return fmt.Errorf("error slot has no colors")
	}
	if p.Warning.Light == nil && p.Warning.Dark == nil {
		return fmt.Errorf("warning slot has no colors")
	}
	if p.Accent.Light == nil && p.Accent.Dark == nil {
		return fmt.Errorf("accent slot has no colors")
	}
	if p.KeyCap.Light == nil && p.KeyCap.Dark == nil {
		return fmt.Errorf("keyCap slot has no colors")
	}
	if p.Muted.Light == nil && p.Muted.Dark == nil {
		return fmt.Errorf("muted slot has no colors")
	}
	if p.Border.Light == nil && p.Border.Dark == nil {
		return fmt.Errorf("border slot has no colors")
	}
	if p.Background.Light == nil && p.Background.Dark == nil {
		return fmt.Errorf("background slot has no colors")
	}
	return nil
}

const (
	tabLog       = 0
	tabProviders = 1
	tabMappings  = 2
)

const (
	formNone          = 0
	formEditProvider  = 1
	formAddProvider   = 2
	formEditMapping   = 3
	formAddMapping    = 4
	formDeleteConfirm = 5
)
