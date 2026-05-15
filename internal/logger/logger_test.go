package logger

import (
	"testing"
)

func TestSetLevel(t *testing.T) {
	SetLevel("debug")
	Debug("test debug message")
	Info("test info message")

	SetLevel("error")
	Debug("should not appear")
	Info("should not appear")
	Error("should appear")
}

func TestInfo(t *testing.T) {
	// Apenas verifica que não panic
	Info("test", "key", "value")
}

func TestDebug(t *testing.T) {
	SetLevel("debug")
	Debug("debug test", "key1", "val1")
}

func TestWarn(t *testing.T) {
	Warn("warn test", "key", "value")
}

func TestError(t *testing.T) {
	Error("error test", "key", "value", "error", "something")
}

func TestSetLevel_Invalid(t *testing.T) {
	// Nível inválido deve fallback para info
	SetLevel("invalid")
	Info("should appear after invalid level")
}

func TestDebugContext(t *testing.T) {
	SetLevel("debug")
	DebugContext(nil, "debug context test", "key", "value")
}

func TestInfoContext(t *testing.T) {
	InfoContext(nil, "info context test", "key", "value")
}

func TestWarnContext(t *testing.T) {
	WarnContext(nil, "warn context test", "key", "value")
}

func TestErrorContext(t *testing.T) {
	ErrorContext(nil, "error context test", "key", "value")
}

func TestSetLevel_Warn(t *testing.T) {
	SetLevel("warn")
	Warn("warn level test")
	Error("error level test")
	Debug("should not appear at warn level")
	Info("should not appear at warn level")
}

func TestSetLevel_Error(t *testing.T) {
	SetLevel("error")
	Error("error level test only")
	Debug("should not appear")
	Info("should not appear")
	Warn("should not appear")
}
