package logging

import (
	"bytes"
	"testing"

	"go.uber.org/zap/zapcore"
)

// logr's verbosity lands on zap's negative levels, and V(2) is the custom trace
// level, which zap would otherwise render as "Level(-2)".
func TestLogrVerboseLevelRendersAsTrace(t *testing.T) {
	var buf bytes.Buffer
	NewLogr(NewZapLogger(&buf, ToZapLevel(TraceLevel), true)).V(2).Info("cache sync")

	if got := decodeRecord(t, &buf)["level"]; got != "TRACE" {
		t.Fatalf("level = %v, want TRACE", got)
	}
}

func TestLogrRendersTheSameShapeAsSlog(t *testing.T) {
	var logrBuf, slogBuf bytes.Buffer
	NewLogr(NewZapLogger(&logrBuf, zapcore.InfoLevel, true)).Info("starting manager")
	NewSlog(NewZapLogger(&slogBuf, zapcore.InfoLevel, true)).Info("starting manager")

	fromLogr, fromSlog := decodeRecord(t, &logrBuf), decodeRecord(t, &slogBuf)
	for _, key := range []string{"level", "msg"} {
		if fromLogr[key] != fromSlog[key] {
			t.Fatalf("%q differs between the logr and slog adapters: %v vs %v", key, fromLogr, fromSlog)
		}
	}
	if _, ok := fromLogr["time"]; !ok {
		t.Fatalf("the logr adapter must carry \"time\" too: %v", fromLogr)
	}
}
