package logging

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newTestZapLogger builds a ZapLogger writing to buf at the given level.
// The console core uses ToZapLevel + EncodeLevelWithTrace so the test
// exercises the full pipeline.
func newTestZapLogger(buf *bytes.Buffer, level Level) (*ZapLogger, *threadSafeSyncer) {
	sink := &threadSafeSyncer{buf: buf}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.EncodeLevel = EncodeLevelWithTrace(zapcore.LowercaseLevelEncoder)
	enc := zapcore.NewJSONEncoder(encCfg)

	core := zapcore.NewCore(enc, sink, ToZapLevel(level))
	return NewZap(zap.New(core).Sugar()), sink
}

// threadSafeSyncer wraps a bytes.Buffer with a mutex; zapcore.AddSync
// alone doesn't synchronise concurrent writes, which trips the race
// detector when the Writer() goroutine logs in parallel with assertions.
type threadSafeSyncer struct {
	mu  sync.Mutex
	buf *bytes.Buffer
}

func (s *threadSafeSyncer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *threadSafeSyncer) Sync() error { return nil }

func (s *threadSafeSyncer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestZapLoggerTraceEmittedAtTraceLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, sink := newTestZapLogger(&buf, TraceLevel)

	logger.Trace("trace-msg")
	logger.Tracef("trace-fmt-%d", 42)
	logger.Debug("debug-msg")
	logger.Info("info-msg")

	out := sink.String()
	for _, want := range []string{"trace-msg", "trace-fmt-42", "debug-msg", "info-msg", `"level":"trace"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}

func TestZapLoggerTraceSilentAtDebugLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, sink := newTestZapLogger(&buf, DebugLevel)

	logger.Trace("should-not-appear")
	logger.Tracef("nor-%s", "this")
	logger.Debug("debug-still-emitted")

	out := sink.String()
	if strings.Contains(out, "should-not-appear") || strings.Contains(out, "nor-this") {
		t.Errorf("trace records leaked into Debug-level output:\n%s", out)
	}
	if !strings.Contains(out, "debug-still-emitted") {
		t.Errorf("debug record missing from output:\n%s", out)
	}
}

func TestZapLoggerEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configured Level
		check      Level
		want       bool
	}{
		{TraceLevel, TraceLevel, true},
		{TraceLevel, DebugLevel, true},
		{TraceLevel, InfoLevel, true},
		{DebugLevel, TraceLevel, false},
		{DebugLevel, DebugLevel, true},
		{InfoLevel, DebugLevel, false},
		{InfoLevel, InfoLevel, true},
		{ErrorLevel, InfoLevel, false},
		{ErrorLevel, ErrorLevel, true},
	}

	for _, tc := range cases {
		var buf bytes.Buffer
		logger, _ := newTestZapLogger(&buf, tc.configured)
		if got := logger.Enabled(tc.check); got != tc.want {
			t.Errorf("Enabled(%v) on level=%v = %v, want %v", tc.check, tc.configured, got, tc.want)
		}
	}
}

func TestZapLoggerWithFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base, sink := newTestZapLogger(&buf, TraceLevel)

	child := base.WithFields(map[string]any{"req_id": "abc", "n": 7})
	child.Tracef("tagged")

	out := sink.String()
	for _, want := range []string{`"req_id":"abc"`, `"n":7`, "tagged"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}

func TestNopZap(t *testing.T) {
	t.Parallel()

	l := NopZap()
	// All methods should be safe to call and produce no output.
	l.Trace("x")
	l.Tracef("x-%d", 1)
	l.Debug("x")
	l.Info("x")
	l.Error("x")
	l.WithField("k", "v").Trace("y")
	if l.Enabled(ErrorLevel) {
		t.Errorf("NopZap should report all levels as disabled")
	}
}

// TestMinLevelCoreFiltersTrace verifies the canonical use case: a console
// core accepting Trace, a second core (e.g. OTel bridge) wrapped with
// MinLevelCore at DebugLevel rejecting Trace. Trace records must reach
// the console only.
func TestMinLevelCoreFiltersTrace(t *testing.T) {
	t.Parallel()

	var consoleBuf, otelBuf bytes.Buffer
	consoleSink := &threadSafeSyncer{buf: &consoleBuf}
	otelSink := &threadSafeSyncer{buf: &otelBuf}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.EncodeLevel = EncodeLevelWithTrace(zapcore.LowercaseLevelEncoder)
	enc := zapcore.NewJSONEncoder(encCfg)

	consoleCore := zapcore.NewCore(enc, consoleSink, ToZapLevel(TraceLevel))
	otelCore := &MinLevelCore{
		Core: zapcore.NewCore(enc, otelSink, ToZapLevel(TraceLevel)),
		Min:  zapcore.DebugLevel,
	}

	logger := NewZap(zap.New(zapcore.NewTee(consoleCore, otelCore)).Sugar())

	logger.Trace("trace-only-on-console")
	logger.Debug("debug-on-both")

	consoleOut := consoleSink.String()
	otelOut := otelSink.String()

	if !strings.Contains(consoleOut, "trace-only-on-console") {
		t.Errorf("console core missed trace record:\n%s", consoleOut)
	}
	if strings.Contains(otelOut, "trace-only-on-console") {
		t.Errorf("MinLevelCore failed to drop trace from OTel core:\n%s", otelOut)
	}
	if !strings.Contains(consoleOut, "debug-on-both") || !strings.Contains(otelOut, "debug-on-both") {
		t.Errorf("debug record should reach both cores; console=%q otel=%q", consoleOut, otelOut)
	}
}
