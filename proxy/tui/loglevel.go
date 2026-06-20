package tui

import "log/slog"

// LogFilter represents a level-based filter for the TUI Log tab.
// When Min is nil, all levels are shown ("All" mode).
type LogFilter struct {
	Label string
	Min   *slog.Level
}

var (
	slogLevelError = slog.LevelError
	filterAll      = LogFilter{Label: "all", Min: nil}
	filterError    = LogFilter{Label: "error", Min: &slogLevelError}
)

// Matches reports whether the given level passes this filter.
func (f LogFilter) Matches(level slog.Level) bool {
	return f.Min == nil || level >= *f.Min
}
