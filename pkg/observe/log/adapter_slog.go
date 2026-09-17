package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
)

// TraceHandler wraps an slog.Handler and, when the record's context carries a
// valid span, adds trace_id and span_id taken from that span, so a log line can
// be correlated with the distributed trace it was emitted under.
//
// It is an slog.Handler rather than a zapcore.Core because a Core sees no
// context: zap's API carries none, whereas every slog call site passes one.
// Trace correlation therefore has to be stamped above the bridge to zap.
//
// It keeps the caller's groups and attributes itself rather than delegating
// them to the wrapped handler, and replays them as one nested attribute at
// Handle time. Delegating would put the trace ids inside whatever group was
// open -- a logger built with WithGroup("request") emitted them under
// "request", where nothing querying trace_id at the record root would find
// them.
type TraceHandler struct {
	inner slog.Handler

	// goas records WithGroup and WithAttrs calls in the order they were made,
	// which is the only way to reproduce their nesting faithfully.
	goas []groupOrAttrs
}

// groupOrAttrs holds either an open group or a set of attributes, never both.
type groupOrAttrs struct {
	group string
	attrs []slog.Attr
}

// NewTraceHandler wraps inner with trace correlation.
func NewTraceHandler(inner slog.Handler) *TraceHandler {
	return &TraceHandler{inner: inner}
}

func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TraceHandler) Handle(ctx context.Context, record slog.Record) error {
	out := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)

	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		out.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}

	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)

		return true
	})

	// The encoder's own keys are escaped by reservedFieldCore, which sees every
	// façade. trace_id and span_id are escaped here instead, because this
	// handler is what injects them -- the core cannot tell an application
	// attribute named trace_id from the one stamped just above.
	//
	// Root only: an attribute inside a group is namespaced by it and cannot
	// collide with anything written at the record root.
	rooted := nest(h.goas, attrs)
	for i := range rooted {
		rooted[i].Key = escapeTraceKey(rooted[i].Key)
	}

	out.AddAttrs(rooted...)

	return h.inner.Handle(ctx, out)
}

// escapeTraceKey renames an application attribute that would collide with the
// correlation fields this handler stamps. It renames whether or not a span is
// active, so a field's path does not depend on whether the request happened to
// be traced.
func escapeTraceKey(key string) string {
	switch key {
	case "trace_id", "span_id":
		return "fields." + key
	default:
		return key
	}
}

// nest replays the recorded groups and attributes around the record's own
// attributes, innermost first, so the result is what the wrapped handler would
// have produced had it been given the groups directly.
func nest(goas []groupOrAttrs, tail []slog.Attr) []slog.Attr {
	for i := len(goas) - 1; i >= 0; i-- {
		if goas[i].group == "" {
			tail = append(append([]slog.Attr{}, goas[i].attrs...), tail...)

			continue
		}

		// slog's contract: a group that would be empty is not emitted at all.
		if len(tail) == 0 {
			continue
		}

		tail = []slog.Attr{{Key: goas[i].group, Value: slog.GroupValue(tail...)}}
	}

	return tail
}

func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	return h.with(groupOrAttrs{attrs: attrs})
}

func (h *TraceHandler) WithGroup(name string) slog.Handler {
	// slog's contract: an empty group name is a no-op.
	if name == "" {
		return h
	}

	return h.with(groupOrAttrs{group: name})
}

func (h *TraceHandler) with(goa groupOrAttrs) *TraceHandler {
	goas := make([]groupOrAttrs, len(h.goas)+1)
	copy(goas, h.goas)
	goas[len(goas)-1] = goa

	return &TraceHandler{inner: h.inner, goas: goas}
}

// NewSlog exposes z as an *slog.Logger, for application code written against
// the standard library. Records go through z's core, so they share the
// encoder, the writer and the level of every other adapter over the same
// logger.
//
// otelTraces attaches trace correlation, and is the same condition
// NewDefaultLogger takes for the logrus hook: whether the service has a traces
// exporter configured. pkg/service derives it from
// otlptraces.OtelTracesExporterFlag. Stamping ids for a trace no backend will
// receive correlates a record with nothing, which is why it is a condition
// rather than a default.
//
// It is a parameter rather than a separate constructor so a caller cannot pick
// the uncorrelated one by accident: the choice has to be made, and it is made
// from the same value that governs the other two stacks.
func NewSlog(z *zap.Logger, otelTraces bool) *slog.Logger {
	handler := baseSlogHandler(z)
	if otelTraces {
		handler = NewTraceHandler(handler)
	}

	return slog.New(handler)
}

// baseSlogHandler bridges to zap, carrying across what the core cannot know:
// the logger name, which belongs to the *zap.Logger rather than to its core.
//
// It also disables zapslog's stack trace at Error and above. slog never
// attached one, and turning it on would add a Go stack to each error line a
// busy service emits, including this package's own. A caller that wants them
// builds its own handler with zapslog.AddStacktraceAt.
func baseSlogHandler(z *zap.Logger) slog.Handler {
	return zapslog.NewHandler(
		z.Core(),
		zapslog.WithName(z.Name()),
		zapslog.AddStacktraceAt(slog.LevelError+1),
	)
}

// slogAdapter implements Logger on top of an *slog.Logger.
type slogAdapter struct {
	logger *slog.Logger
	ctx    context.Context
	writer io.Writer
}

var _ Logger = (*slogAdapter)(nil)

// NewSlogLogger wraps an *slog.Logger as a Logger, for code that already has
// one -- a package built on the standard library, or a caller composing its own
// slog handlers -- and needs to hand it to something taking a Logger, such as
// service.NewWithLogger.
//
// Reach for NewZap instead when starting from the *zap.Logger: it keeps the
// custom trace level, which this path cannot -- zapslog clamps every slog level
// below Info to Debug, so a Trace record arrives at Debug.
func NewSlogLogger(logger *slog.Logger) Logger {
	return &slogAdapter{
		logger: logger,
		ctx:    context.Background(),
		writer: NewLineWriter(logger, slog.LevelInfo),
	}
}

func levelToSlog(level Level) slog.Level {
	switch level {
	case ErrorLevel:
		return slog.LevelError
	case InfoLevel:
		return slog.LevelInfo
	case DebugLevel, TraceLevel:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func (a *slogAdapter) log(level Level, msg string) {
	a.logger.Log(a.ctx, levelToSlog(level), msg)
}

func (a *slogAdapter) Tracef(format string, args ...any) {
	a.log(TraceLevel, fmt.Sprintf(format, args...))
}

func (a *slogAdapter) Debugf(format string, args ...any) {
	a.log(DebugLevel, fmt.Sprintf(format, args...))
}

func (a *slogAdapter) Infof(format string, args ...any) {
	a.log(InfoLevel, fmt.Sprintf(format, args...))
}

func (a *slogAdapter) Errorf(format string, args ...any) {
	a.log(ErrorLevel, fmt.Sprintf(format, args...))
}

func (a *slogAdapter) Trace(args ...any) { a.log(TraceLevel, fmt.Sprint(args...)) }
func (a *slogAdapter) Debug(args ...any) { a.log(DebugLevel, fmt.Sprint(args...)) }
func (a *slogAdapter) Info(args ...any)  { a.log(InfoLevel, fmt.Sprint(args...)) }
func (a *slogAdapter) Error(args ...any) { a.log(ErrorLevel, fmt.Sprint(args...)) }

func (a *slogAdapter) WithFields(fields map[string]any) Logger {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}

	return a.with(a.logger.With(args...))
}

func (a *slogAdapter) WithField(key string, value any) Logger {
	return a.with(a.logger.With(key, value))
}

func (a *slogAdapter) WithContext(ctx context.Context) Logger {
	return a.derive(a.logger, ctx)
}

func (a *slogAdapter) with(logger *slog.Logger) Logger {
	return a.derive(logger, a.ctx)
}

// derive rebuilds the writer rather than carrying the parent's. The writer
// emits through a context of its own, so reusing it would leave records a
// third-party library writes during a traced request unstamped while direct
// calls on the same adapter carry trace_id and span_id.
func (a *slogAdapter) derive(logger *slog.Logger, ctx context.Context) Logger {
	return &slogAdapter{
		logger: logger,
		ctx:    ctx,
		writer: newLineWriter(logger, slog.LevelInfo, ctx),
	}
}

// Writer returns an io.Writer that turns each line written to it into a record,
// rather than the raw sink the logger was built on. A third-party library handed
// this writer therefore produces encoded records like everything else, instead
// of emitting unstructured text into the middle of the stream.
func (a *slogAdapter) Writer() io.Writer {
	return a.writer
}

func (a *slogAdapter) Enabled(level Level) bool {
	return a.logger.Enabled(a.ctx, levelToSlog(level))
}

// LineWriter turns each line written to it into one record at a fixed level. It
// exists for libraries that only accept an io.Writer or a *log.Logger: without
// it, their output is the one part of a service's stream that is not structured
// at all.
type LineWriter struct {
	logger *slog.Logger
	level  slog.Level
	ctx    context.Context

	// mu guards buf across the whole buffer-and-emit cycle. Writer() hands the
	// same LineWriter to arbitrary callers, and a *log.Logger built on it can
	// be shared by several goroutines, so locking only the underlying sink
	// would still leave this buffer racing -- and a race here does not merely
	// interleave output, it merges unrelated records or drops them.
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewLineWriter builds a LineWriter emitting at level.
func NewLineWriter(logger *slog.Logger, level slog.Level) *LineWriter {
	return newLineWriter(logger, level, context.Background())
}

// WithContext returns a LineWriter emitting under ctx, so records written
// through it are stamped with the active span like every other record.
//
// The returned writer starts with an empty buffer: a partial line already held
// by the receiver belongs to whatever was writing it, not to this context.
func (w *LineWriter) WithContext(ctx context.Context) *LineWriter {
	return newLineWriter(w.logger, w.level, ctx)
}

func newLineWriter(logger *slog.Logger, level slog.Level, ctx context.Context) *LineWriter {
	return &LineWriter{logger: logger, level: level, ctx: ctx}
}

// Write buffers p and emits one record per complete line. A trailing partial
// line is held until its newline arrives, so a library writing a record in
// several calls still produces a single record.
func (w *LineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)

	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// No newline yet: put the partial line back and wait for the rest.
			w.buf.Reset()
			w.buf.WriteString(line)

			break
		}

		if trimmed := bytes.TrimRight([]byte(line), "\r\n"); len(trimmed) > 0 {
			w.logger.Log(w.ctx, w.level, string(trimmed))
		}
	}

	return len(p), nil
}
