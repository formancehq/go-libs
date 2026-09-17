package logging

import (
	"io"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ParseZapLevel parses a textual level into a zapcore.Level, defaulting to Info
// for anything unrecognised or empty rather than refusing to start.
//
// It accepts "warn", which Level does not have: zap has a real Warn level, so a
// service asking for it gets it instead of the nearest rounding.
func ParseZapLevel(s string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return ToZapLevel(TraceLevel)
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// ZapLevelFromFlags resolves the effective level from a textual --log-level
// value and the --debug flag. debug clamps the level to at least Debug
// verbosity, matching the default logrus logger, which honours --debug the same
// way. It is needed because service.NewWithLogger bypasses that default logger
// entirely, and because a binary exposing only --debug has no other way to
// reach the verbose internal logs.
func ZapLevelFromFlags(logLevel string, debug bool) zapcore.Level {
	level := ParseZapLevel(logLevel)
	if debug && level > zapcore.DebugLevel {
		return zapcore.DebugLevel
	}

	return level
}

// ZapEncoderConfig is the record shape every Formance service emits.
//
// The keys deliberately match what a slog logger writes rather than what zap
// defaults to -- "time" and not "ts", RFC 3339 with nanoseconds, capitalised
// levels, durations as integer nanoseconds. A deployment usually runs several
// services and several logger stacks side by side; if the timestamp lands under
// a different key per binary, nothing downstream can query the set as one.
func ZapEncoderConfig() zapcore.EncoderConfig {
	cfg := zap.NewProductionEncoderConfig()
	cfg.TimeKey = "time"
	cfg.LevelKey = "level"
	cfg.MessageKey = "msg"
	cfg.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	cfg.EncodeLevel = encodeZapLevel
	cfg.EncodeDuration = zapcore.NanosDurationEncoder

	return cfg
}

// encodeZapLevel renders levels capitalised, and the custom trace level as
// "TRACE". EncodeLevelWithTrace renders it lowercase, which would make it the
// one record in the stream whose level is cased differently.
func encodeZapLevel(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	if l == ToZapLevel(TraceLevel) {
		enc.AppendString("TRACE")

		return
	}

	zapcore.CapitalLevelEncoder(l, enc)
}

// NewZapLogger builds the process-wide logger. Records are encoded as JSON when
// jsonFormatting is set -- the JSON_FORMATTING_LOGGER knob -- and in zap's
// console format otherwise.
//
// Expose the result through the adapters in this package rather than building a
// second logger: NewZap for this package's Logger, NewSlog for slog call sites,
// NewLogr for controller-runtime and klog. All of them write through this core,
// so the choice of interface stays invisible in the output.
func NewZapLogger(w io.Writer, level zapcore.Level, jsonFormatting bool) *zap.Logger {
	cfg := ZapEncoderConfig()

	var encoder zapcore.Encoder
	if jsonFormatting {
		encoder = zapcore.NewJSONEncoder(cfg)
	} else {
		encoder = zapcore.NewConsoleEncoder(cfg)
	}

	// zapcore.Lock, not just AddSync: AddSync only supplies a no-op Sync when
	// the writer has none, it serializes nothing, and NewCore requires a sink
	// that is safe for concurrent use. zap's own documentation singles out
	// *os.File as needing the lock, and this constructor additionally accepts
	// any io.Writer -- a bytes.Buffer in tests, a bufio.Writer in a caller --
	// none of which tolerate concurrent writes from several goroutines.
	// The renaming lives in a core rather than in one adapter, so it covers
	// every façade over this logger -- NewSlog, NewLogr, NewZap and any direct
	// zap use -- instead of whichever one remembered to apply it.
	core := zapcore.NewCore(encoder, zapcore.Lock(zapcore.AddSync(w)), level)

	return zap.New(&reservedFieldCore{Core: core})
}

// reservedKeys are the keys ZapEncoderConfig writes itself. An application
// field using one would make the record carry it twice, and a consumer keeping
// the last value would lose the actual message -- or the severity, the
// timestamp, or the logger name.
var reservedKeys = [...]string{"level", "time", "msg", "logger"}

// emittedFieldName renames a field that would collide with one of those.
//
// logrus.JSONFormatter made the same substitution for level, time and msg, so
// a record that used to read "fields.msg" still does. "logger" is added
// because ZapEncoderConfig emits it and logrus never did.
func emittedFieldName(key string) string {
	for _, reserved := range reservedKeys {
		if key == reserved {
			return "fields." + key
		}
	}

	return key
}

// reservedFieldCore renames colliding field keys on their way to the encoder.
//
// Putting it here rather than in an adapter is what makes the guarantee hold
// across the whole stack: zapr forwards WithValues keys straight through, and
// NewZap hands SugaredLogger keys over untouched, so any adapter-level
// renaming protects one façade and silently misses the others.
type reservedFieldCore struct {
	zapcore.Core
}

func (c *reservedFieldCore) With(fields []zapcore.Field) zapcore.Core {
	return &reservedFieldCore{Core: c.Core.With(renameReserved(fields))}
}

// Check must add this core rather than the embedded one, or the entry is
// written straight to the inner core and the renaming never runs.
func (c *reservedFieldCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}

	return ce
}

func (c *reservedFieldCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	return c.Core.Write(ent, renameReserved(fields))
}

// renameReserved escapes reserved keys, giving the escaped reserved field
// precedence over a field already named "fields.<key>". Without that rule a
// record carrying both "msg" and "fields.msg" would emit "fields.msg" twice and
// let the argument order decide the winner, which is the ambiguity the renaming
// exists to remove.
func renameReserved(fields []zapcore.Field) []zapcore.Field {
	escaped := false
	for i := range fields {
		if emittedFieldName(fields[i].Key) != fields[i].Key {
			escaped = true

			break
		}
	}

	if !escaped {
		return fields
	}

	renamed := make(map[string]struct{}, len(reservedKeys))
	for _, f := range fields {
		if name := emittedFieldName(f.Key); name != f.Key {
			renamed[name] = struct{}{}
		}
	}

	out := make([]zapcore.Field, 0, len(fields))
	for _, f := range fields {
		name := emittedFieldName(f.Key)
		if name == f.Key {
			// A literal "fields.msg" yields to the escaped reserved field.
			if _, taken := renamed[f.Key]; taken {
				continue
			}
		}

		f.Key = name
		out = append(out, f)
	}

	return out
}
