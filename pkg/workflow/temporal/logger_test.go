package temporal

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestKeyvalsToMapHandlesOddAndNonStringKeys(t *testing.T) {
	t.Parallel()

	var got map[string]any
	require.NotPanics(t, func() {
		got = keyvalsToMap("workflow", "payment", 42, "answer", "dangling")
	})

	require.Equal(t, map[string]any{
		"workflow": "payment",
		"42":       "answer",
	}, got)
}

func TestLoggerWarnUsesWarnLevel(t *testing.T) {
	t.Parallel()

	recorder := newRecordingLogger()

	newLogger(recorder).Warn("temporal warning", "attempt", 3)

	require.Len(t, recorder.state.entries, 1)
	require.Equal(t, recordedLogEntry{
		level:  "warn",
		msg:    "temporal warning",
		fields: map[string]any{"attempt": 3},
	}, recorder.state.entries[0])
}

type recordedLogEntry struct {
	level  string
	msg    string
	fields map[string]any
}

type recordingLoggerState struct {
	entries []recordedLogEntry
}

type recordingLogger struct {
	state  *recordingLoggerState
	fields map[string]any
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{
		state:  &recordingLoggerState{},
		fields: map[string]any{},
	}
}

func (l *recordingLogger) Tracef(format string, args ...any) {
	l.record("trace", fmt.Sprintf(format, args...))
}

func (l *recordingLogger) Debugf(format string, args ...any) {
	l.record("debug", fmt.Sprintf(format, args...))
}

func (l *recordingLogger) Infof(format string, args ...any) {
	l.record("info", fmt.Sprintf(format, args...))
}

func (l *recordingLogger) Warnf(format string, args ...any) {
	l.record("warn", fmt.Sprintf(format, args...))
}

func (l *recordingLogger) Errorf(format string, args ...any) {
	l.record("error", fmt.Sprintf(format, args...))
}

func (l *recordingLogger) Trace(args ...any) {
	l.record("trace", fmt.Sprint(args...))
}

func (l *recordingLogger) Debug(args ...any) {
	l.record("debug", fmt.Sprint(args...))
}

func (l *recordingLogger) Info(args ...any) {
	l.record("info", fmt.Sprint(args...))
}

func (l *recordingLogger) Error(args ...any) {
	l.record("error", fmt.Sprint(args...))
}

func (l *recordingLogger) WithFields(fields map[string]any) logging.Logger {
	merged := l.cloneFields()
	for key, value := range fields {
		merged[key] = value
	}
	return &recordingLogger{
		state:  l.state,
		fields: merged,
	}
}

func (l *recordingLogger) WithField(key string, value any) logging.Logger {
	return l.WithFields(map[string]any{key: value})
}

func (l *recordingLogger) WithContext(context.Context) logging.Logger {
	return l
}

func (l *recordingLogger) Writer() io.Writer {
	return io.Discard
}

func (l *recordingLogger) Enabled(logging.Level) bool {
	return true
}

func (l *recordingLogger) record(level, msg string) {
	l.state.entries = append(l.state.entries, recordedLogEntry{
		level:  level,
		msg:    msg,
		fields: l.cloneFields(),
	})
}

func (l *recordingLogger) cloneFields() map[string]any {
	fields := make(map[string]any, len(l.fields))
	for key, value := range l.fields {
		fields[key] = value
	}
	return fields
}
