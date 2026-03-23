package logging

import (
	"context"
	"io"
)

// Level represents a logging severity level.
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	ErrorLevel
)

//go:generate mockgen -source logging.go -destination logging_generated.go -package logging . Logger
type Logger interface {
	Debugf(fmt string, args ...any)
	Infof(fmt string, args ...any)
	Errorf(fmt string, args ...any)
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

func Debugf(fmt string, args ...any) {
	FromContext(context.TODO()).Debugf(fmt, args...)
}
func Infof(fmt string, args ...any) {
	FromContext(context.TODO()).Infof(fmt, args...)
}
func Errorf(fmt string, args ...any) {
	FromContext(context.TODO()).Errorf(fmt, args...)
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
