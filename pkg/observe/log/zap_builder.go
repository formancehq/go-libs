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
// The fallback is silent, which is a trade: a mistyped --log-level ("erro")
// changes verbosity without saying so, but a pod does not crash-loop over a
// typo in a log knob. Use ParseLevel when a caller wants the error instead.
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
	// At or below trace, not just equal to it: zapr maps logr's V(n) onto
	// zapcore.Level(-n), so a controller-runtime or klog call site at V(3) or
	// beyond reaches a level CapitalLevelEncoder spells "LEVEL(-3)". One
	// record in a namespace carrying that instead of a word breaks the closed
	// vocabulary the rest of this stack maintains.
	//
	// Verbosity beyond trace therefore renders as TRACE rather than as its own
	// spelling. Those records become indistinguishable by level, which they
	// already were in practice -- everything below Debug is trace detail.
	if l <= ToZapLevel(TraceLevel) {
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
// second logger: NewZap for this package's Logger, NewLogr for
// controller-runtime and klog. All of them write through this core,
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
	// every façade over this logger -- NewZap, NewLogr and any direct zap use --
	// instead of whichever one remembered to apply it.
	core := zapcore.NewCore(encoder, zapcore.Lock(zapcore.AddSync(w)), level)

	return zap.New(&reservedFieldCore{Core: core})
}

// reservedKeys are the keys ZapEncoderConfig writes itself. An application
// field using one would make the record carry it twice, and a consumer keeping
// the last value would lose the actual message -- or the severity, the
// timestamp, or the logger name.
var reservedKeys = [...]string{"level", "time", "msg", "logger"}

// traceKeys are the correlation ids a correlating logger stamps. An
// application field using one of them is escaped the same way a reserved key
// is -- see escapeTraceKey.
var traceKeys = [...]string{"trace_id", "span_id"}

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

	// escaped records the keys already held by the inner core as a result of
	// escaping. A field scoped with With is encoded by that core, so a later
	// Write cannot see it -- without this, z.With(zap.String("msg", …)) and a
	// later literal "fields.msg" both encode under the same key and the
	// precedence rule silently stops holding across the two calls.
	escaped map[string]struct{}

	// namespaced is set once a namespace has been opened. Everything after it
	// is nested under that name and cannot collide with a key written at the
	// record root, so escaping it would rename a field for no reason -- and
	// rename it differently depending on which façade opened the group.
	namespaced bool
}

func (c *reservedFieldCore) With(fields []zapcore.Field) zapcore.Core {
	if c.namespaced {
		return &reservedFieldCore{Core: c.Core.With(fields), escaped: c.escaped, namespaced: true}
	}

	// Fields reaching With are always application fields: the correlation ids
	// a correlating ZapLogger stamps arrive on the record, through Write. So this is the
	// one place where trace_id and span_id can be escaped without risking the
	// stamped pair.
	renamed := renameReserved(fields, scopedName)

	escaped := make(map[string]struct{}, len(c.escaped)+len(renamed))
	for k := range c.escaped {
		escaped[k] = struct{}{}
	}

	// Derived from the keys rather than by pairing fields with renamed by
	// index: renameReserved drops a literal "fields.msg" that yields to an
	// escaped "msg", so its result can be shorter than its input. Pairing by
	// index read past the end and panicked -- in a logging core, on a field
	// name an application chooses.
	for _, f := range fields {
		// Everything from the first namespace on is nested under it, out of
		// reach of the record's own keys, and renameReserved leaves it alone.
		if f.Type == zapcore.NamespaceType {
			break
		}

		if name := escapeScoped(f.Key); name != f.Key {
			escaped[name] = struct{}{}
		}
	}

	// A literal key the inner core already holds as an escaped field would be
	// encoded twice, the same collision Write resolves -- and here the literal
	// arrives second, so a last-value decoder would read it rather than the
	// reserved field the escaping exists to protect.
	renamed = dropShadowedLiterals(renamed, c.escaped)

	return &reservedFieldCore{
		Core:       c.Core.With(renamed),
		escaped:    escaped,
		namespaced: opensNamespace(fields),
	}
}

// dropShadowedLiterals removes a literal "fields.<key>" whose slot the inner
// core already holds as an escaped reserved field, which would otherwise encode
// the key twice.
//
// It never compacts in place. renameReserved can return a slice aliasing the
// one it was given -- when nothing needed escaping it appends the namespaced
// tail onto the caller's own array -- so writing through it corrupted the
// caller's fields, which for Write are the record's. A probe caught that: a
// caller's slice came back with its first element overwritten by its second.
func dropShadowedLiterals(fields []zapcore.Field, escaped map[string]struct{}) []zapcore.Field {
	if len(escaped) == 0 {
		return fields
	}

	shadowed := func(f zapcore.Field) bool {
		_, taken := escaped[f.Key]

		return taken && escapeScoped(f.Key) == f.Key
	}

	// Scanning first keeps the ordinary record allocation-free: nothing is
	// dropped unless a literal actually collides.
	drop := false
	for _, f := range fields {
		if shadowed(f) {
			drop = true

			break
		}
	}

	if !drop {
		return fields
	}

	kept := make([]zapcore.Field, 0, len(fields))
	for _, f := range fields {
		if shadowed(f) {
			continue
		}

		kept = append(kept, f)
	}

	return kept
}

// opensNamespace reports whether these fields leave a namespace open, in which
// case everything that follows is nested under it.
func opensNamespace(fields []zapcore.Field) bool {
	for _, f := range fields {
		if f.Type == zapcore.NamespaceType {
			return true
		}
	}

	return false
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
	if c.namespaced {
		return c.Core.Write(ent, fields)
	}

	// Only the encoder's own keys here: the record may carry the correlation
	// ids a correlating logger just stamped, and nothing at this level distinguishes
	// them from an application field of the same name.
	out := renameReserved(fields, recordName)

	// A literal key the inner core already holds as an escaped field would
	// encode twice.
	out = dropShadowedLiterals(out, c.escaped)

	return c.Core.Write(ent, out)
}

// escapeTraceKey renames an application field that would collide with the
// correlation ids a trace-correlating logger stamps.
//
// It renames on every zap-stack path, not only the correlating one, and whether
// or not a span is active: scoping it to NewZapWithTraces would make a field's
// path depend on whether the service configured a traces exporter, and on
// whether the request happened to be sampled.
func escapeTraceKey(key string) string {
	for _, id := range traceKeys {
		if key == id {
			return "fields." + key
		}
	}

	return key
}

// scopedName is the escaping applied to a field scoped with With. Everything
// reaching that path is an application field: the pair a correlating logger
// stamps arrives on the record instead.
func scopedName(f zapcore.Field) string { return escapeScoped(f.Key) }

// recordName is the escaping applied to a field on the record. It escapes the
// correlation ids like any other application field -- a call site can write
// them through Infow or zapr's WithValues just as easily as through With -- but
// leaves the pair the logger itself stamped alone, which is the one case where
// those keys are not an application field.
func recordName(f zapcore.Field) string {
	if stampedCorrelation(f) {
		return f.Key
	}

	return escapeScoped(f.Key)
}

// escapeScoped escapes both the encoder's keys and the correlation ids, for
// fields scoped ahead of the record.
func escapeScoped(key string) string {
	if name := emittedFieldName(key); name != key {
		return name
	}

	return escapeTraceKey(key)
}

// renameReserved escapes reserved keys, giving the escaped reserved field
// precedence over a field already named "fields.<key>". Without that rule a
// record carrying both "msg" and "fields.msg" would emit "fields.msg" twice and
// let the argument order decide the winner, which is the ambiguity the renaming
// exists to remove.
func renameReserved(fields []zapcore.Field, escape func(zapcore.Field) string) []zapcore.Field {
	// Everything from the first namespace on is nested under it and out of
	// reach of the record's own keys.
	root := len(fields)
	for i, f := range fields {
		if f.Type == zapcore.NamespaceType {
			root = i

			break
		}
	}

	nested := fields[root:]
	fields = fields[:root]

	found := false
	for i := range fields {
		if escape(fields[i]) != fields[i].Key {
			found = true

			break
		}
	}

	if !found {
		return append(fields, nested...)
	}

	renamed := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if name := escape(f); name != f.Key {
			renamed[name] = struct{}{}
		}
	}

	out := make([]zapcore.Field, 0, len(fields))
	for _, f := range fields {
		name := escape(f)
		if name == f.Key {
			// A literal "fields.msg" yields to the escaped reserved field.
			if _, taken := renamed[f.Key]; taken {
				continue
			}
		}

		f.Key = name
		out = append(out, f)
	}

	return append(out, nested...)
}
