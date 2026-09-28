package logging

import (
	"bytes"
	"testing"
)

func TestLogrusLoggerWarnMethodsEmitError(t *testing.T) {
	for _, emit := range []func(*LogrusLogger){
		func(l *LogrusLogger) { l.Warn("warning") },
		func(l *LogrusLogger) { l.Warnf("warning %d", 1) },
	} {
		var buf bytes.Buffer
		emit(NewDefaultLoggerWithLevel(&buf, ErrorLevel, true, false))

		if got := decodeRecord(t, &buf)["level"]; got != "error" {
			t.Fatalf("level = %v, want error", got)
		}
	}
}
