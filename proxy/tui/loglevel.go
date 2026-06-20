package tui

import "log/slog"

// LogFilter represents a level-based filter for the TUI Log tab.
// When Min is nil, all levels are shown ("All" mode).
type LogFilter struct {
	Label string
	Min   *slog.Level
}

var (
	slogLevelDebug = slog.LevelDebug
	slogLevelInfo  = slog.LevelInfo
	slogLevelWarn  = slog.LevelWarn
	slogLevelError = slog.LevelError

	filterAll   = LogFilter{Label: "all", Min: nil}
	filterDebug = LogFilter{Label: "debug", Min: &slogLevelDebug}
	filterInfo  = LogFilter{Label: "info", Min: &slogLevelInfo}
	filterWarn  = LogFilter{Label: "warn", Min: &slogLevelWarn}
	filterError = LogFilter{Label: "error", Min: &slogLevelError}
)

var logFilterCycle = []LogFilter{filterAll, filterDebug, filterInfo, filterWarn, filterError}

// Matches reports whether the given level passes this filter.
func (f LogFilter) Matches(level slog.Level) bool {
	return f.Min == nil || level >= *f.Min
}
