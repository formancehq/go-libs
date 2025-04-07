package logging

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoggerFunctions(t *testing.T) {
	logger := Testing()

	logger.Debugf("debug %s", "message")
	logger.Debug("debug", "message")
	logger.Infof("info %s", "message")
	logger.Info("info", "message")
	logger.Errorf("error %s", "message")
	logger.Error("error", "message")

	loggerWithFields := logger.WithFields(map[string]any{"key": "value"})
	require.NotNil(t, loggerWithFields, "WithFields devrait retourner un logger")

	loggerWithField := logger.WithField("key", "value")
	require.NotNil(t, loggerWithField, "WithField devrait retourner un logger")

	loggerWithContext := logger.WithContext(context.Background())
	require.NotNil(t, loggerWithContext, "WithContext devrait retourner un logger")
}

func TestNewDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewDefaultLogger(&buf, true, false, false)
	require.NotNil(t, logger, "NewDefaultLogger devrait retourner un logger")

	logger.Info("test message")
	require.Contains(t, buf.String(), "test message", "Le message devrait être écrit dans le buffer")
}

type TestLogger struct {
	fields map[string]any
}

func (l *TestLogger) Debugf(fmt string, args ...any) {}
func (l *TestLogger) Infof(fmt string, args ...any)  {}
func (l *TestLogger) Errorf(fmt string, args ...any) {}
func (l *TestLogger) Debug(args ...any)              {}
func (l *TestLogger) Info(args ...any)               {}
func (l *TestLogger) Error(args ...any)              {}
func (l *TestLogger) WithFields(fields map[string]any) Logger {
	return &TestLogger{fields: fields}
}
func (l *TestLogger) WithField(key string, value any) Logger {
	return l.WithFields(map[string]any{key: value})
}
func (l *TestLogger) WithContext(ctx context.Context) Logger {
	return l
}
func (l *TestLogger) Writer() io.Writer {
	return io.Discard
}
