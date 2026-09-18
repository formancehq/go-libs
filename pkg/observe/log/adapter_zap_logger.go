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
	// to correlate against. Nothing else is taken from it, and it is only
	// consulted when correlate is set.
	ctx context.Context

	// correlate is what NewZapWithTraces turns on. It is off by default so
	// NewZap keeps the behaviour it has always had.
	correlate bool
}

var _ Logger = (*ZapLogger)(nil)

// NewZap wraps a *zap.SugaredLogger as a Logger.
//
// Its records carry no trace correlation: WithContext returns the receiver, as
// it always has. Correlation is attached deliberately, the way SetHooks
// attaches it on the logrus side -- use NewZapWithTraces.
func NewZap(sugar *zap.SugaredLogger) *ZapLogger {
	return &ZapLogger{sugar: sugar}
}

// NewZapWithTraces wraps a *zap.SugaredLogger as a Logger whose records carry
// the ids of the span the context holds, when it holds one.
//
// It is the zap counterpart of the correlation SetHooks attaches to a logrus
// logger, and it belongs at the same place in a service's wiring: alongside a
// configured traces exporter, on the same condition. Stamping ids for a trace
// no backend will ever receive correlates a record with nothing.
//
// It is a constructor rather than a hook or a core because a zapcore.Core sees
// no context -- which is what "an attached otelzap core" meant, otelzap being
// the bridge that carries one. Taking the context through Logger.WithContext
// achieves the same without the dependency.
func NewZapWithTraces(sugar *zap.SugaredLogger) *ZapLogger {
	return &ZapLogger{sugar: sugar, correlate: true}
}

// NopZap returns a Logger backed by zap.NewNop() — useful in tests and
// short-lived CLI commands that need a Logger but discard everything.
func NopZap() *ZapLogger {
	return &ZapLogger{sugar: zap.NewNop().Sugar()}
}

func (z *ZapLogger) Tracef(format string, args ...any) { z.logf(zapTraceLevel, format, args...) }
func (z *ZapLogger) Debugf(format string, args ...any) {
	z.logf(zapcore.DebugLevel, format, args...)
}
func (z *ZapLogger) Infof(format string, args ...any) { z.logf(zapcore.InfoLevel, format, args...) }
func (z *ZapLogger) Warnf(format string, args ...any) { z.logf(zapcore.WarnLevel, format, args...) }
func (z *ZapLogger) Errorf(format string, args ...any) {
	z.logf(zapcore.ErrorLevel, format, args...)
}

func (z *ZapLogger) Trace(args ...any) { z.log(zapTraceLevel, args...) }
func (z *ZapLogger) Debug(args ...any) { z.log(zapcore.DebugLevel, args...) }
func (z *ZapLogger) Info(args ...any)  { z.log(zapcore.InfoLevel, args...) }
func (z *ZapLogger) Warn(args ...any)  { z.log(zapcore.WarnLevel, args...) }
func (z *ZapLogger) Error(args ...any) { z.log(zapcore.ErrorLevel, args...) }

// logf and log emit the record, stamped with the active span's ids when the
// context this logger carries has one.
//
// A logger that does not correlate -- which is every NewZap logger, and the
// hot path -- hands the arguments to zap unformatted, exactly as this adapter
// did before correlation existed. zap checks the level first and never formats
// a suppressed record, which is what makes the documented
//
//	if logger.Enabled(logging.DebugLevel) { ... }
//
// guard an optimisation rather than a necessity. Formatting eagerly here would
// have charged every suppressed Debugf in a hot loop for a string nobody reads.
//
// The correlating path cannot use that call: it has fields to attach, so it
// checks the level itself before formatting.
//
// The stamping happens here rather than in WithContext so the fields reach the
// core through Write rather than With. That distinction is load-bearing:
// reservedFieldCore escapes an application field named trace_id on the With
// path, and must not escape the pair stamped here.
func (z *ZapLogger) logf(level zapcore.Level, format string, args ...any) {
	if !z.correlate {
		z.sugar.Logf(level, format, args...)

		return
	}

	if !z.sugar.Desugar().Core().Enabled(level) {
		return
	}

	z.sugar.Desugar().Log(level, fmt.Sprintf(format, args...), z.correlation()...)
}

func (z *ZapLogger) log(level zapcore.Level, args ...any) {
	if !z.correlate {
		z.sugar.Log(level, args...)

		return
	}

	if !z.sugar.Desugar().Core().Enabled(level) {
		return
	}

	z.sugar.Desugar().Log(level, fmt.Sprint(args...), z.correlation()...)
}

// correlation returns the ids of the span carried by this logger's context, or
// nothing when there is no context or no valid span in it -- an unsampled or
// untraced record gains no fields.
func (z *ZapLogger) correlation() []zap.Field {
	if !z.correlate || z.ctx == nil {
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

	return &ZapLogger{sugar: z.sugar.With(kvs...), ctx: z.ctx, correlate: z.correlate}
}

func (z *ZapLogger) WithField(key string, value any) Logger {
	return &ZapLogger{sugar: z.sugar.With(key, value), ctx: z.ctx, correlate: z.correlate}
}

// WithContext returns a logger carrying ctx, whose records are stamped with the
// ids of the span it holds -- but only on a logger built by NewZapWithTraces.
//
// A logger from NewZap returns the receiver, unchanged, as it always has. That
// is deliberate rather than an omission: correlation is attached explicitly on
// the logrus side too, by SetHooks, and only when a traces exporter is
// configured.
func (z *ZapLogger) WithContext(ctx context.Context) Logger {
	if !z.correlate {
		return z
	}

	return &ZapLogger{sugar: z.sugar, ctx: ctx, correlate: true}
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
