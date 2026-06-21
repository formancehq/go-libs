package logging

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"go.uber.org/zap"
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
	z.sugar.Logf(zapTraceLevel, format, args...)
}
func (z *ZapLogger) Trace(args ...any)                 { z.sugar.Log(zapTraceLevel, args...) }
func (z *ZapLogger) Debugf(format string, args ...any) { z.sugar.Debugf(format, args...) }
func (z *ZapLogger) Infof(format string, args ...any)  { z.sugar.Infof(format, args...) }
func (z *ZapLogger) Warnf(format string, args ...any)  { z.sugar.Warnf(format, args...) }
func (z *ZapLogger) Errorf(format string, args ...any) { z.sugar.Errorf(format, args...) }
func (z *ZapLogger) Debug(args ...any)                 { z.sugar.Debug(args...) }
func (z *ZapLogger) Info(args ...any)                  { z.sugar.Info(args...) }
func (z *ZapLogger) Warn(args ...any)                  { z.sugar.Warn(args...) }
func (z *ZapLogger) Error(args ...any)                 { z.sugar.Error(args...) }

func (z *ZapLogger) Enabled(level Level) bool {
	return z.sugar.Desugar().Core().Enabled(ToZapLevel(level))
}

func (z *ZapLogger) WithFields(fields map[string]any) Logger {
	kvs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		kvs = append(kvs, k, v)
	}

	return &ZapLogger{sugar: z.sugar.With(kvs...)}
}

func (z *ZapLogger) WithField(key string, value any) Logger {
	return &ZapLogger{sugar: z.sugar.With(key, value)}
}

// WithContext returns self; OTel correlation is expected to be handled by
// an attached otelzap core (or equivalent bridge) rather than by re-wrapping
// the logger per call.
func (z *ZapLogger) WithContext(_ context.Context) Logger {
	return z
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
