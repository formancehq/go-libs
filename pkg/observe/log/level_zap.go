package logging

import (
	"go.uber.org/zap/zapcore"
)

// zapTraceLevel is the custom zapcore.Level used to emit TraceLevel records.
// It sits at -2, one notch below zapcore.DebugLevel (-1), so any zapcore.Core
// configured to accept Trace will also accept Debug and above.
const zapTraceLevel zapcore.Level = -2

// ToZapLevel maps a logging.Level to its zapcore counterpart. TraceLevel
// maps to the custom zapTraceLevel; unknown levels fall back to InfoLevel.
func ToZapLevel(level Level) zapcore.Level {
	switch level {
	case TraceLevel:
		return zapTraceLevel
	case DebugLevel:
		return zapcore.DebugLevel
	case InfoLevel:
		return zapcore.InfoLevel
	case ErrorLevel:
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// EncodeLevelWithTrace wraps a zapcore.LevelEncoder so the custom Trace level
// is rendered as "trace" instead of falling back to zap's default formatting
// (which would print something like "Level(-2)").
func EncodeLevelWithTrace(base zapcore.LevelEncoder) zapcore.LevelEncoder {
	return func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		if l == zapTraceLevel {
			enc.AppendString("trace")
			return
		}
		base(l, enc)
	}
}

// MinLevelCore wraps a zapcore.Core and rejects log entries below a minimum
// level. Typical use: prevent Trace records from reaching an OTel exporter
// core while still allowing them on the console core. Compose with
// zapcore.NewTee to build multi-destination logging pipelines.
//
//	consoleCore := zapcore.NewCore(enc, console, logging.ToZapLevel(level))
//	otelCore := &logging.MinLevelCore{Core: otelBridge, Min: zapcore.DebugLevel}
//	root := zap.New(zapcore.NewTee(consoleCore, otelCore))
type MinLevelCore struct {
	zapcore.Core
	Min zapcore.Level
}

func (c *MinLevelCore) Enabled(lvl zapcore.Level) bool {
	if lvl < c.Min {
		return false
	}
	return c.Core.Enabled(lvl)
}

func (c *MinLevelCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if ent.Level < c.Min {
		return ce
	}
	return c.Core.Check(ent, ce)
}

func (c *MinLevelCore) With(fields []zapcore.Field) zapcore.Core {
	return &MinLevelCore{Core: c.Core.With(fields), Min: c.Min}
}
