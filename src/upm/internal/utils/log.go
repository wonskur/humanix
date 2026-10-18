package utils

import (
	"log/slog"
	"os"
)

var LogLevel = new(slog.LevelVar)

// InitLogger sets up slog with appropriate log level and formatting.
func InitLogger(verbose, quiet bool) {
	if quiet {
		LogLevel.Set(slog.LevelError)
	} else if verbose {
		LogLevel.Set(slog.LevelDebug)
	} else {
		LogLevel.Set(slog.LevelInfo)
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: LogLevel,
	})
	slog.SetDefault(slog.New(handler))
}
