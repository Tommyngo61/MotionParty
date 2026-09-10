// Package logging builds the agent's structured logger.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New builds a logger.
//
// Output goes to stdout, which systemd captures into the journal. The agent
// does not manage its own log files: log rotation on a machine we cannot reach,
// with a disk that filling up is the top real-world failure mode, is a job for
// journald's size limits rather than something to reimplement.
func New(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: ParseLevel(level)}

	var h slog.Handler
	if strings.EqualFold(format, "text") {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// ParseLevel maps a config string to a level, defaulting to info.
func ParseLevel(v string) slog.Level {
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
