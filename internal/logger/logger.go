package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// defaultLogger is the package-level logger instance.
var defaultLogger *slog.Logger

// init initializes the default logger with sensible defaults.
func init() {
	SetLevel("info")
}

// SetLevel configures the default logger with the given level.
// Valid levels: debug, info, warn, error.
func SetLevel(level string) {
	var slogLevel slog.Level

	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel,
	})

	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// Debug logs a message at debug level.
func Debug(msg string, args ...any) {
	defaultLogger.Debug(msg, args...)
}

// Info logs a message at info level.
func Info(msg string, args ...any) {
	defaultLogger.Info(msg, args...)
}

// Warn logs a message at warn level.
func Warn(msg string, args ...any) {
	defaultLogger.Warn(msg, args...)
}

// Error logs a message at error level.
func Error(msg string, args ...any) {
	defaultLogger.Error(msg, args...)
}

// DebugContext logs a message at debug level with context.
func DebugContext(ctx context.Context, msg string, args ...any) {
	defaultLogger.DebugContext(ctx, msg, args...)
}

// InfoContext logs a message at info level with context.
func InfoContext(ctx context.Context, msg string, args ...any) {
	defaultLogger.InfoContext(ctx, msg, args...)
}

// WarnContext logs a message at warn level with context.
func WarnContext(ctx context.Context, msg string, args ...any) {
	defaultLogger.WarnContext(ctx, msg, args...)
}

// ErrorContext logs a message at error level with context.
func ErrorContext(ctx context.Context, msg string, args ...any) {
	defaultLogger.ErrorContext(ctx, msg, args...)
}
