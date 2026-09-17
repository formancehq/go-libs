package logging

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapLogger adapts a *zap.SugaredLogger to the Logger interface.
//
// Use NewZap to wrap an existing sugared logger, or NopZap for a silent
// logger (mainly in tests and CLI subcommands that don't need output).
//
// Trace records are emitted at a custom zapcore.Level (see zapTraceLevel
// in level_zap.go), one notch below DebugLevel. Cores that don't enable
// that level — including OTel exporters wrapped with a MinLevelCore floor
// at DebugLevel — will drop trace records, which is by design: trace is
// meant for local stdout-only diagnostics.
type ZapLogger struct {
	sugar *zap.SugaredLogger

	// ctx is the context WithContext was given, read at log time for the span
	// to correlate against. Nothing else is taken from it.
	ctx context.Context
}

var _ Logger = (*ZapLogger)(nil)

// NewZap wraps a *zap.SugaredLogger as a Logger.
func NewZap(sugar *zap.SugaredLogger) *ZapLogger {
	return &ZapLogger{sugar: sugar}
}

// NopZap returns a Logger backed by zap.NewNop() — useful in tests and
// short-lived CLI commands that need a Logger but discard everything.
func NopZap() *ZapLogger {
	return &ZapLogger{sugar: zap.NewNop().Sugar()}
}

func (z *ZapLogger) Tracef(format string, args ...any) {
	z.log(zapTraceLevel, fmt.Sprintf(format, args...))
}
func (z *ZapLogger) Debugf(format string, args ...any) {
	z.log(zapcore.DebugLevel, fmt.Sprintf(format, args...))
}
func (z *ZapLogger) Infof(format string, args ...any) {
	z.log(zapcore.InfoLevel, fmt.Sprintf(format, args...))
}
func (z *ZapLogger) Warnf(format string, args ...any) {
	z.log(zapcore.WarnLevel, fmt.Sprintf(format, args...))
}
func (z *ZapLogger) Errorf(format string, args ...any) {
	z.log(zapcore.ErrorLevel, fmt.Sprintf(format, args...))
}

func (z *ZapLogger) Trace(args ...any) { z.log(zapTraceLevel, fmt.Sprint(args...)) }
func (z *ZapLogger) Debug(args ...any) { z.log(zapcore.DebugLevel, fmt.Sprint(args...)) }
func (z *ZapLogger) Info(args ...any)  { z.log(zapcore.InfoLevel, fmt.Sprint(args...)) }
func (z *ZapLogger) Warn(args ...any)  { z.log(zapcore.WarnLevel, fmt.Sprint(args...)) }
func (z *ZapLogger) Error(args ...any) { z.log(zapcore.ErrorLevel, fmt.Sprint(args...)) }

// log emits the record, stamped with the active span's ids when the context
// this logger carries has one.
//
// The stamping happens here rather than in WithContext so the fields reach the
// core through Write rather than With. That distinction is load-bearing:
// reservedFieldCore escapes an application field named trace_id on the With
// path, and must not escape the pair stamped here.
func (z *ZapLogger) log(level zapcore.Level, msg string) {
	if fields := z.correlation(); len(fields) > 0 {
		z.sugar.Desugar().Log(level, msg, fields...)

		return
	}

	z.sugar.Log(level, msg)
}

// correlation returns the ids of the span carried by this logger's context, or
// nothing when there is no context or no valid span in it -- an unsampled or
// untraced record gains no fields.
func (z *ZapLogger) correlation() []zap.Field {
	if z.ctx == nil {
		return nil
	}

	sc := trace.SpanContextFromContext(z.ctx)
	if !sc.IsValid() {
		return nil
	}

	return []zap.Field{
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	}
}

func (z *ZapLogger) Enabled(level Level) bool {
	return z.sugar.Desugar().Core().Enabled(ToZapLevel(level))
}

func (z *ZapLogger) WithFields(fields map[string]any) Logger {
	kvs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		kvs = append(kvs, k, v)
	}

	return &ZapLogger{sugar: z.sugar.With(kvs...), ctx: z.ctx}
}

func (z *ZapLogger) WithField(key string, value any) Logger {
	return &ZapLogger{sugar: z.sugar.With(key, value), ctx: z.ctx}
}

// WithContext returns a logger whose records carry the trace and span ids of
// the span ctx holds, if it holds one. A context with no span, or an invalid
// one, adds nothing.
//
// It used to return the receiver and defer correlation to "an attached otelzap
// core", which no build of this module has ever attached -- so every caller
// reaching this through ContextWithLogger, the HTTP middleware included, asked
// for correlation and silently got none.
func (z *ZapLogger) WithContext(ctx context.Context) Logger {
	return &ZapLogger{sugar: z.sugar, ctx: ctx}
}

// Writer returns an io.Writer that logs each scanned line at InfoLevel.
// Useful for adapting third-party loggers that only accept an io.Writer.
func (z *ZapLogger) Writer() io.Writer {
	pr, pw := io.Pipe()

	go func() {
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			z.sugar.Info(scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			z.sugar.Errorf("log writer scanner error: %v", err)
		}
	}()

	return pw
}

// Zap returns the underlying *zap.Logger. Use this when interfacing with
// libraries that require a zap.Logger directly (e.g. etcd WAL).
func (z *ZapLogger) Zap() *zap.Logger {
	return z.sugar.Desugar()
}

// String implements fmt.Stringer for debug purposes.
func (z *ZapLogger) String() string {
	return fmt.Sprintf("ZapLogger{%v}", z.sugar)
}
