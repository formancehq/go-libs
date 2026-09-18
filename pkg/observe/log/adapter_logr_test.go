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

func TestLogrRendersTheSameShapeAsTheLoggerFacade(t *testing.T) {
	var logrBuf, zapBuf bytes.Buffer
	NewLogr(NewZapLogger(&logrBuf, zapcore.InfoLevel, true)).Info("starting manager")
	NewZap(NewZapLogger(&zapBuf, zapcore.InfoLevel, true).Sugar()).Infof("starting manager")

	fromLogr, fromZap := decodeRecord(t, &logrBuf), decodeRecord(t, &zapBuf)
	for _, key := range []string{"level", "msg"} {
		if fromLogr[key] != fromZap[key] {
			t.Fatalf("%q differs between the logr and Logger adapters: %v vs %v", key, fromLogr, fromZap)
		}
	}
	if _, ok := fromLogr["time"]; !ok {
		t.Fatalf("the logr adapter must carry \"time\" too: %v", fromLogr)
	}
}

// Regression: zapr maps logr's V(n) onto zapcore.Level(-n), and anything below
// the custom trace level rendered as "LEVEL(-3)" -- a record whose level is not
// one of the words every other record carries.
func TestLogrVerbosityBeyondTraceStillRendersAWord(t *testing.T) {
	for _, v := range []int{2, 3, 4, 9} {
		var buf bytes.Buffer
		NewLogr(NewZapLogger(&buf, zapcore.Level(-10), true)).V(v).Info("cache sync")

		if got := decodeRecord(t, &buf)["level"]; got != "TRACE" {
			t.Fatalf("V(%d): level = %v, want TRACE", v, got)
		}
	}
}
