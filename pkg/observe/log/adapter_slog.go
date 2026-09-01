package logging

import (
	"context"
	"log/slog"
	"slices"
)

// NewSlogHandler adapts a Logger to the standard library's slog.Handler, so
// code written against slog flows through the platform logger instead of
// slog's stock stderr text handler:
//
//	slog.SetDefault(slog.New(logging.NewSlogHandler(logger)))
//
// Existing slog call sites need no change; they gain the platform logger's
// formatting (--json-formatting-logger), its level (--debug) and — because
// Handle binds the logger to the call's context — the OpenTelemetry hook that
// stamps trace_id/span_id.
//
// A nil logger discards instead of panicking: a logging call is the worst
// possible place to take a process down.
func NewSlogHandler(logger Logger) slog.Handler {
	return &slogHandler{logger: logger}
}

// slogField is an attribute flattened for emission: a fully group-qualified
// key and an already-resolved value.
type slogField struct {
	key   string
	value any
}

type slogHandler struct {
	// logger is nil for a discarding handler.
	logger Logger
	// prefix is the chain of names opened by WithGroup, dot-joined and
	// dot-terminated ("G." or "G.H."), or empty at the top level.
	prefix string
	// attrs holds the attributes accumulated by WithAttrs, flattened and
	// group-qualified when they were added.
	attrs []slogField
}

var _ slog.Handler = (*slogHandler)(nil)

// Enabled consults the underlying logger, so --debug actually gates slog
// debug calls rather than every record being built and then dropped.
func (h *slogHandler) Enabled(_ context.Context, level slog.Level) bool {
	if h.logger == nil {
		return false
	}

	return h.logger.Enabled(fromSlogLevel(level))
}

func (h *slogHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.logger == nil {
		return nil
	}

	// Bind the logger to this call's context. The logrus OTel hook reads the
	// active span from it, which is what correlates the line with the trace;
	// a context with no recording span simply adds no trace fields.
	logger := h.logger.WithContext(ctx)

	if fields := h.fields(record); len(fields) > 0 {
		logger = logger.WithFields(fields)
	}

	// record.Time is deliberately dropped: emission goes through Logger, whose
	// signature is Info(args ...any), so there is no way to carry a timestamp,
	// and the backing logger stamps its own anyway. See TestSlogHandlerConformance
	// for the one slogtest case this costs.
	switch {
	case record.Level >= slog.LevelError:
		logger.Error(record.Message)
	case record.Level >= slog.LevelWarn:
		// Warn is why Logger grew Warn/Warnf (see the Logger interface).
		// Folding warnings into Error would turn every warning into an alert.
		logger.Warn(record.Message)
	case record.Level >= slog.LevelInfo:
		logger.Info(record.Message)
	case record.Level >= slog.LevelDebug:
		logger.Debug(record.Message)
	default:
		logger.Trace(record.Message)
	}

	return nil
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	clone := h.clone()
	for _, attr := range attrs {
		flattenSlogAttr(h.prefix, attr, func(key string, value any) {
			clone.attrs = append(clone.attrs, slogField{key: key, value: value})
		})
	}

	return clone
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	// The slog.Handler contract: an empty name returns the receiver unchanged.
	if name == "" {
		return h
	}

	clone := h.clone()
	clone.prefix += name + "."

	return clone
}

// clone copies the accumulated attributes rather than sharing the slice.
// slog branches handlers — one logger yields several via WithAttrs — and two
// branches appending into a shared backing array would overwrite each other's
// attributes.
func (h *slogHandler) clone() *slogHandler {
	return &slogHandler{
		logger: h.logger,
		prefix: h.prefix,
		attrs:  slices.Clone(h.attrs),
	}
}

// fields merges the handler's accumulated attributes with the record's own.
func (h *slogHandler) fields(record slog.Record) map[string]any {
	fields := make(map[string]any, len(h.attrs)+record.NumAttrs())
	for _, field := range h.attrs {
		fields[field.key] = field.value
	}

	record.Attrs(func(attr slog.Attr) bool {
		flattenSlogAttr(h.prefix, attr, func(key string, value any) {
			fields[key] = value
		})
		return true
	})

	return fields
}

// flattenSlogAttr emits attr as zero or more flat key/value pairs, each key
// qualified with the enclosing group names.
//
// Groups are flattened into dotted keys ("G.H.key") rather than nested maps:
// Logger carries flat fields, and flat scalar keys are what log search
// backends index well. It implements the attribute half of the slog.Handler
// contract: resolve LogValuer values, ignore the empty Attr, ignore empty
// groups (including their name), and inline a group with an empty name.
func flattenSlogAttr(prefix string, attr slog.Attr, emit func(key string, value any)) {
	attr.Value = attr.Value.Resolve()

	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		if len(group) == 0 {
			return
		}
		if attr.Key != "" {
			prefix += attr.Key + "."
		}
		for _, nested := range group {
			flattenSlogAttr(prefix, nested, emit)
		}

		return
	}

	// The zero Attr. Spelled out rather than using attr.Equal(slog.Attr{})
	// because slog.Value.Equal compares the underlying values with ==, which
	// panics on non-comparable types.
	if attr.Key == "" && attr.Value.Any() == nil {
		return
	}

	emit(prefix+attr.Key, attr.Value.Any())
}

// fromSlogLevel maps a slog.Level onto the closest Level.
//
// slog.LevelWarn maps to InfoLevel: Level has no warn of its own, and this is
// only used by Enabled. The mapping is deliberately permissive — a logger that
// emits Info also emits Warn, and the backing logger filters again at emit, so
// an over-generous answer costs a wasted field build, never a line that should
// have been suppressed getting through.
func fromSlogLevel(level slog.Level) Level {
	switch {
	case level < slog.LevelDebug:
		return TraceLevel
	case level < slog.LevelInfo:
		return DebugLevel
	case level < slog.LevelError:
		return InfoLevel
	default:
		return ErrorLevel
	}
}
