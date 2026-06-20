package tui

import (
	lipgloss "charm.land/lipgloss/v2"
)

var (
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Underline(true).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Faint(true).
				Padding(0, 1)

	statusOKStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2"))

	statusClientErrStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("3"))

	statusErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("1"))

	statsBarStyle = lipgloss.NewStyle().
			Reverse(true).
			Padding(0, 1)

	tabBarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("8"))

	windowStyle = lipgloss.NewStyle().
			Padding(0, 1)

	providerTableHeaderStyle = lipgloss.NewStyle().
					Bold(true).
					Underline(true)

	configKeyStyle = lipgloss.NewStyle().
			Bold(true)

	configValueStyle = lipgloss.NewStyle().
				Faint(true)

	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))
)

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
