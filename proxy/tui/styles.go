package tui

import (
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

// Theme pairs a human-readable name with a full color Palette.
type Theme struct {
	Name    string
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
	return Theme{Name: "default", Palette: DefaultPalette()}
}

var themeRegistry = []Theme{
	DefaultTheme(),
}

// resolveTheme scans themeRegistry for a matching name. If not found it
// returns the first (default) entry — unknown theme names silently fall back.
func resolveTheme(name string) *Theme {
	for i := range themeRegistry {
		if themeRegistry[i].Name == name {
			return &themeRegistry[i]
		}
	}
	return &themeRegistry[0]
}

// NewStyles builds a complete Styles struct from a palette. Each color slot
// is resolved through lipgloss.LightDark(isDark) so the active variant
// matches the terminal background.
func NewStyles(p Palette, isDark bool) Styles {
	fn := lipgloss.LightDark(isDark)
	return Styles{
		ActiveTabStyle: lipgloss.NewStyle().
			Bold(true).
			Underline(true).
			Padding(0, 1),
		InactiveTabStyle: lipgloss.NewStyle().
			Faint(true).
			Padding(0, 1),
		StatusClientErrStyle: lipgloss.NewStyle().
			Foreground(fn(p.Warning.Light, p.Warning.Dark)),
		StatusErrorStyle: lipgloss.NewStyle().
			Foreground(fn(p.Error.Light, p.Error.Dark)),
		StatsBarStyle: lipgloss.NewStyle().
			Reverse(true).
			Padding(0, 1),
		TabBarStyle: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(fn(p.Border.Light, p.Border.Dark)),
		WindowStyle: lipgloss.NewStyle().
			Padding(0, 1),
		ProviderTableHeaderStyle: lipgloss.NewStyle().
			Bold(true).
			Underline(true),
		ConfigKeyStyle: lipgloss.NewStyle().
			Bold(true),
		ConfigValueStyle: lipgloss.NewStyle().
			Faint(true),
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
	}
}

const (
	tabLog       = 0
	tabProviders = 1
	tabConfig    = 2
)

const (
	formNone          = 0
	formEditProvider  = 1
	formAddProvider   = 2
	formEditMapping   = 3
	formAddMapping    = 4
	formDeleteConfirm = 5
)
