package logging

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Level represents a logging severity level. Lower values are more verbose.
type Level int

const (
	TraceLevel Level = iota - 1 // -1, more verbose than Debug; reserved for per-event/per-request logs.
	DebugLevel                  // 0
	InfoLevel                   // 1
	ErrorLevel                  // 2
)

// ParseLevel parses a textual level (case-insensitive) into a Level.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return TraceLevel, nil
	case "debug":
		return DebugLevel, nil
	case "info":
		return InfoLevel, nil
	case "error":
		return ErrorLevel, nil
	default:
		return 0, fmt.Errorf("unknown log level: %q (expected trace|debug|info|error)", s)
	}
}

// String returns the textual representation of a Level.
func (l Level) String() string {
	switch l {
	case TraceLevel:
		return "trace"
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case ErrorLevel:
		return "error"
	default:
		return fmt.Sprintf("level(%d)", int(l))
	}
}

//go:generate mockgen -source logging.go -destination logging_generated.go -package logging . Logger
type Logger interface {
	Tracef(fmt string, args ...any)
	Debugf(fmt string, args ...any)
	Infof(fmt string, args ...any)
	Errorf(fmt string, args ...any)
	Trace(args ...any)
	Debug(args ...any)
	Info(args ...any)
	Error(args ...any)
	WithFields(map[string]any) Logger
	WithField(key string, value any) Logger
	WithContext(ctx context.Context) Logger
	Writer() io.Writer
	// Enabled reports whether the logger will emit logs at the given level.
	// Use this to guard expensive log calls and avoid unnecessary allocations:
	//
	//   if logger.Enabled(logging.DebugLevel) {
	//       logger.WithFields(map[string]any{...}).Debugf("...")
	//   }
	Enabled(level Level) bool
}

func Tracef(format string, args ...any) {
	FromContext(context.TODO()).Tracef(format, args...)
}
func Debugf(format string, args ...any) {
	FromContext(context.TODO()).Debugf(format, args...)
}
func Infof(format string, args ...any) {
	FromContext(context.TODO()).Infof(format, args...)
}
func Errorf(format string, args ...any) {
	FromContext(context.TODO()).Errorf(format, args...)
}
func Trace(args ...any) {
	FromContext(context.TODO()).Trace(args...)
}
func Debug(args ...any) {
	FromContext(context.TODO()).Debug(args...)
}
func Info(args ...any) {
	FromContext(context.TODO()).Info(args...)
}
func Error(args ...any) {
	FromContext(context.TODO()).Error(args...)
}
func WithFields(fields map[string]any) Logger {
	return FromContext(context.TODO()).WithFields(fields)
}
