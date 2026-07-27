package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// newSlogTestLogger builds a JSON-formatting LogrusLogger writing to buf.
// Logrus' JSON keys happen to be exactly slog's: time, level, msg.
func newSlogTestLogger(buf *bytes.Buffer, level Level, otelTraces bool) *LogrusLogger {
	return NewDefaultLoggerWithLevel(buf, level, true, otelTraces)
}

// decodeLines parses the JSON lines written to buf.
func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	raw := strings.TrimSpace(buf.String())
	if raw == "" {
		return nil
	}

	lines := strings.Split(raw, "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		entry := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "line: %s", line)
		out = append(out, entry)
	}

	return out
}

// decodeLine parses the single JSON line written to buf.
func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	lines := decodeLines(t, buf)
	require.Len(t, lines, 1)

	return lines[0]
}

// nestGroups turns the handler's dot-qualified keys ("G.H.e") back into the
// nested maps slogtest expects. The handler flattens groups on purpose (see
// flattenSlogAttr); slogtest documents that the results function is where a
// handler's own format is translated into the shape the checks assume.
func nestGroups(flat map[string]any) map[string]any {
	nested := map[string]any{}
	for key, value := range flat {
		parts := strings.Split(key, ".")
		target := nested
		for _, part := range parts[:len(parts)-1] {
			child, ok := target[part].(map[string]any)
			if !ok {
				child = map[string]any{}
				target[part] = child
			}
			target = child
		}
		target[parts[len(parts)-1]] = value
	}

	return nested
}

// TestSlogHandlerConformance runs the standard library's own slog.Handler
// conformance suite against the adapter.
func TestSlogHandlerConformance(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	handler := NewSlogHandler(newSlogTestLogger(buf, TraceLevel, false))

	err := slogtest.TestHandler(handler, func() []map[string]any {
		results := decodeLines(t, buf)
		for i, entry := range results {
			results[i] = nestGroups(entry)
		}
		return results
	})

	// The one case that cannot pass. Emission goes through Logger, whose
	// signature is Info(args ...any): there is no way to pass or suppress a
	// timestamp, and the backing logger always stamps its own. That is correct
	// in production and unreachable in practice — slog.Logger always sets
	// Record.Time from the clock, so only a hand-built Record has a zero one.
	//
	// Allow-listed by exact message rather than skipping the suite, so every
	// other case stays enforced and a second deviation fails the test.
	const allowedDeviation = "a Handler should ignore a zero Record.Time"

	var sawAllowedDeviation bool
	for _, problem := range unwrapJoined(err) {
		if strings.Contains(problem.Error(), allowedDeviation) {
			sawAllowedDeviation = true
			continue
		}
		t.Errorf("slogtest: %v", problem)
	}

	require.True(t, sawAllowedDeviation,
		"the documented %q deviation no longer occurs — remove the allow-list", allowedDeviation)
}

// unwrapJoined splits an errors.Join result back into its parts.
func unwrapJoined(err error) []error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}

	return []error{err}
}

// TestSlogHandlerTraceCorrelation is the point of the adapter: a line logged
// through slog inside a span carries the trace, because Handle binds the
// logger to the call's context and the OTel hook reads the span from it.
func TestSlogHandlerTraceCorrelation(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	logger := slog.New(NewSlogHandler(newSlogTestLogger(buf, InfoLevel, true)))

	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	ctx, span := provider.Tracer("slog-adapter-test").Start(context.Background(), "operation")
	logger.InfoContext(ctx, "inside the span", "k", "v")
	span.End()

	entry := decodeLine(t, buf)
	require.Equal(t, "inside the span", entry["msg"])
	require.Equal(t, "v", entry["k"])
	require.Equal(t, span.SpanContext().TraceID().String(), entry["trace_id"])
	require.Equal(t, span.SpanContext().SpanID().String(), entry["span_id"])
}

// TestSlogHandlerNoSpanNoTraceFields: a context with no span produces no trace
// fields, not empty ones.
func TestSlogHandlerNoSpanNoTraceFields(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	logger := slog.New(NewSlogHandler(newSlogTestLogger(buf, InfoLevel, true)))

	logger.InfoContext(context.Background(), "no span here")

	entry := decodeLine(t, buf)
	require.NotContains(t, entry, "trace_id")
	require.NotContains(t, entry, "span_id")
}

func TestSlogHandlerLevelGating(t *testing.T) {
	t.Parallel()

	t.Run("without debug", func(t *testing.T) {
		t.Parallel()

		buf := &bytes.Buffer{}
		handler := NewSlogHandler(newSlogTestLogger(buf, InfoLevel, false))

		require.False(t, handler.Enabled(context.Background(), slog.LevelDebug))
		require.True(t, handler.Enabled(context.Background(), slog.LevelInfo))
		// Warn maps to InfoLevel for the Enabled check — deliberately permissive.
		require.True(t, handler.Enabled(context.Background(), slog.LevelWarn))
		require.True(t, handler.Enabled(context.Background(), slog.LevelError))

		slog.New(handler).Debug("suppressed")
		require.Empty(t, buf.String())
	})

	t.Run("with debug", func(t *testing.T) {
		t.Parallel()

		buf := &bytes.Buffer{}
		handler := NewSlogHandler(newSlogTestLogger(buf, DebugLevel, false))

		require.True(t, handler.Enabled(context.Background(), slog.LevelDebug))

		slog.New(handler).Debug("emitted")
		require.Equal(t, "emitted", decodeLine(t, buf)["msg"])
	})
}

// TestSlogHandlerWarnIsNotError pins the Warn decision: a slog warning is
// emitted at the backing logger's warn level, not folded into Error.
func TestSlogHandlerWarnIsNotError(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	slog.New(NewSlogHandler(newSlogTestLogger(buf, InfoLevel, false))).Warn("careful")

	entry := decodeLine(t, buf)
	require.Equal(t, "careful", entry["msg"])
	require.Equal(t, "warning", entry["level"]) // logrus spells warn "warning"
}

func TestSlogHandlerLevelMapping(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	logger := slog.New(NewSlogHandler(newSlogTestLogger(buf, TraceLevel, false)))

	logger.Log(context.Background(), slog.LevelDebug-4, "trace")
	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")

	levels := make([]string, 0, 5)
	for _, entry := range decodeLines(t, buf) {
		levels = append(levels, entry["level"].(string))
	}

	require.Equal(t, []string{"trace", "debug", "info", "warning", "error"}, levels)
}

// TestSlogHandlerWithAttrsBranchesAreIndependent guards the append-aliasing
// trap: slog branches handlers, and two branches appending into a shared
// backing array would overwrite each other's attributes.
func TestSlogHandlerWithAttrsBranchesAreIndependent(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}

	// Build the root in several steps so its attrs slice has spare capacity —
	// that is the state in which a missing copy actually corrupts a sibling.
	root := NewSlogHandler(newSlogTestLogger(buf, InfoLevel, false)).
		WithAttrs([]slog.Attr{slog.String("a", "1")}).
		WithAttrs([]slog.Attr{slog.String("b", "2")}).
		WithAttrs([]slog.Attr{slog.String("c", "3")})

	left := root.WithAttrs([]slog.Attr{slog.String("branch", "left")})
	right := root.WithAttrs([]slog.Attr{slog.String("branch", "right")})

	slog.New(left).Info("left")
	slog.New(right).Info("right")
	slog.New(root).Info("root")

	entries := decodeLines(t, buf)
	require.Len(t, entries, 3)

	for _, entry := range entries {
		require.Equal(t, "1", entry["a"])
		require.Equal(t, "2", entry["b"])
		require.Equal(t, "3", entry["c"])
	}

	require.Equal(t, "left", entries[0]["branch"])
	require.Equal(t, "right", entries[1]["branch"])
	require.NotContains(t, entries[2], "branch", "the root handler must not see a branch's attributes")
}

// TestSlogHandlerWithGroupBranchesAreIndependent: the same for group prefixes.
func TestSlogHandlerWithGroupBranchesAreIndependent(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	root := NewSlogHandler(newSlogTestLogger(buf, InfoLevel, false)).
		WithAttrs([]slog.Attr{slog.String("shared", "yes")})

	slog.New(root.WithGroup("G")).Info("grouped", "k", "v")
	slog.New(root).Info("ungrouped", "k", "v")

	entries := decodeLines(t, buf)
	require.Len(t, entries, 2)

	require.Equal(t, "v", entries[0]["G.k"])
	require.Equal(t, "yes", entries[0]["shared"], "attrs added before WithGroup stay unqualified")
	require.NotContains(t, entries[0], "k")

	require.Equal(t, "v", entries[1]["k"])
	require.NotContains(t, entries[1], "G.k")
}

// TestSlogHandlerNilLoggerDiscards: a logging call is the worst place to take
// a process down.
func TestSlogHandlerNilLoggerDiscards(t *testing.T) {
	t.Parallel()

	handler := NewSlogHandler(nil)

	require.False(t, handler.Enabled(context.Background(), slog.LevelError))
	require.NotPanics(t, func() {
		logger := slog.New(handler).With("a", "b").WithGroup("G")
		logger.Info("discarded", "k", "v")
		logger.Error("discarded too")

		// Handle is also reachable directly, bypassing the Enabled check.
		require.NoError(t, handler.Handle(context.Background(), slog.Record{}))
	})
}
