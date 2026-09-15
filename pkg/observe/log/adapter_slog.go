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
type TraceHandler struct {
	inner slog.Handler
}

// NewTraceHandler wraps inner with trace correlation.
func NewTraceHandler(inner slog.Handler) *TraceHandler {
	return &TraceHandler{inner: inner}
}

func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TraceHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}

	return h.inner.Handle(ctx, record)
}

func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}

// NewSlog exposes z as an *slog.Logger with trace correlation, for application
// code written against the standard library. Records go through z's core, so
// they share the encoder, the writer and the level of every other adapter over
// the same logger.
func NewSlog(z *zap.Logger) *slog.Logger {
	// zapslog attaches a stack trace to every record at Error and above. slog
	// never did, so turning it on here would add a Go stack to each error line
	// a busy service emits, including this package's own. Pointing the
	// threshold one level past Error disables it; a caller that wants stacks
	// can build its own handler with zapslog.AddStacktraceAt.
	return slog.New(NewTraceHandler(zapslog.NewHandler(z.Core(), zapslog.AddStacktraceAt(slog.LevelError+1))))
}

// slogAdapter implements Logger on top of an *slog.Logger.
type slogAdapter struct {
	logger *slog.Logger
	ctx    context.Context
	writer io.Writer
}

var _ Logger = (*slogAdapter)(nil)

// NewSlogLogger wraps an *slog.Logger (as built by NewSlog) as a Logger, ready
// to be handed to service.NewWithLogger.
//
// Prefer it over NewZap when the records must stay trace-correlated: ZapLogger
// carries no context -- its WithContext returns itself and defers correlation
// to an attached otelzap core -- whereas this adapter keeps the context, so
// ContextWithLogger and the HTTP middleware built on it produce records
// stamped with the active span.
//
// The trade-off is Trace: zapslog clamps every slog level below Info to Debug,
// so trace records emitted through this adapter arrive at Debug. Use NewZap
// where the custom trace level matters more than correlation.
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
	return &slogAdapter{logger: a.logger, ctx: ctx, writer: a.writer}
}

func (a *slogAdapter) with(logger *slog.Logger) Logger {
	return &slogAdapter{logger: logger, ctx: a.ctx, writer: NewLineWriter(logger, slog.LevelInfo)}
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
	return &LineWriter{logger: logger, level: level}
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
			w.logger.Log(context.Background(), w.level, string(trimmed))
		}
	}

	return len(p), nil
}
