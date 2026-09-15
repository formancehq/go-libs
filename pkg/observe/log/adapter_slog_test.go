package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"go.uber.org/zap/zapcore"
)

// The point of the adapter: a lifecycle record written through Logger and an
// application record written through slog must be indistinguishable.
func TestSlogLoggerRendersLikeSlogRecords(t *testing.T) {
	var buf bytes.Buffer
	logger := NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true))

	NewSlogLogger(logger).Infof("Starting application")
	lifecycle := decodeRecord(t, &buf)

	buf.Reset()
	logger.Info("source added")
	application := decodeRecord(t, &buf)

	if lifecycle["msg"] != "Starting application" {
		t.Fatalf("unexpected msg: %v", lifecycle)
	}
	if lifecycle["level"] != application["level"] {
		t.Fatalf("levels differ: %v vs %v", lifecycle, application)
	}
	if len(lifecycle) != len(application) {
		t.Fatalf("field sets differ: %v vs %v", lifecycle, application)
	}
}

func TestSlogLoggerWithFieldPropagates(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)))

	adapter.WithField("source", "aws").Infof("batch applied")

	if got := decodeRecord(t, &buf)["source"]; got != "aws" {
		t.Fatalf("source = %v, want aws", got)
	}
}

func TestSlogLoggerWithFieldsPropagates(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)))

	adapter.WithFields(map[string]any{"addr": ":8080", "auth": true}).Info("listening")

	record := decodeRecord(t, &buf)
	if record["addr"] != ":8080" || record["auth"] != true {
		t.Fatalf("fields missing from record: %v", record)
	}
}

// This is what ZapLogger gives up and why this adapter exists: ContextWithLogger
// calls WithContext, so the record carries the span the request runs under.
func TestSlogLoggerWithContextStampsTraceID(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)))

	adapter.WithContext(sampledContext()).Info("Request")

	if got := decodeRecord(t, &buf)["trace_id"]; got != "01000000000000000000000000000000" {
		t.Fatalf("trace_id = %v", got)
	}
}

func TestSlogLoggerContextSurvivesWithField(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)))

	adapter.WithContext(sampledContext()).WithField("request_id", "abc").Info("Request")

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" || record["request_id"] != "abc" {
		t.Fatalf("context or field lost: %v", record)
	}
}

// ContextWithLogger is the path the HTTP middleware uses; it must produce a
// logger whose records are stamped.
func TestContextWithLoggerKeepsTraceCorrelation(t *testing.T) {
	var buf bytes.Buffer
	ctx := ContextWithLogger(sampledContext(), NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true))))

	FromContext(ctx).Info("Request")

	if got := decodeRecord(t, &buf)["trace_id"]; got != "01000000000000000000000000000000" {
		t.Fatalf("trace_id = %v", got)
	}
}

func TestSlogLoggerEnabledRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)))

	if !adapter.Enabled(InfoLevel) {
		t.Fatal("info must be enabled at info level")
	}
	if adapter.Enabled(DebugLevel) {
		t.Fatal("debug must be disabled at info level")
	}
}

// Documented trade-off: zapslog clamps every slog level below Info to Debug, so
// a trace record through this adapter arrives at Debug rather than TRACE.
func TestSlogLoggerCollapsesTraceOntoDebug(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.DebugLevel, true)))

	adapter.Trace("per-event detail")

	if got := decodeRecord(t, &buf)["level"]; got != "DEBUG" {
		t.Fatalf("level = %v, want DEBUG", got)
	}
}

func TestSlogLoggerWriterEmitsOneRecordPerLine(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)))

	if _, err := adapter.Writer().Write([]byte("[DEBUG] GET https://example.test/.well-known/openid-configuration\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	record := decodeRecord(t, &buf)
	if record["msg"] != "[DEBUG] GET https://example.test/.well-known/openid-configuration" {
		t.Fatalf("unexpected msg: %v", record)
	}
	if record["level"] != "INFO" {
		t.Fatalf("level = %v, want INFO", record["level"])
	}
}

func TestLineWriterHoldsPartialLines(t *testing.T) {
	var buf bytes.Buffer
	writer := NewLineWriter(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)), slog.LevelInfo)

	if _, err := writer.Write([]byte("retrying request")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("a line without its newline is not a record yet: %s", buf.String())
	}

	if _, err := writer.Write([]byte(" (attempt 2)\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := decodeRecord(t, &buf)["msg"]; got != "retrying request (attempt 2)" {
		t.Fatalf("msg = %v", got)
	}
}

func TestLineWriterSkipsBlankLines(t *testing.T) {
	var buf bytes.Buffer
	writer := NewLineWriter(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)), slog.LevelInfo)

	if _, err := writer.Write([]byte("\n\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("blank lines must not become records: %s", buf.String())
	}
}

// Regression for a review finding: Writer() hands the same LineWriter to every
// caller, so its buffer needs its own lock -- locking the sink underneath is
// not enough. Fails under -race without the mutex.
func TestLineWriterIsSafeForConcurrentUse(t *testing.T) {
	var buf bytes.Buffer
	writer := NewLineWriter(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)), slog.LevelInfo)

	const writers, perWriter = 8, 50
	var wg sync.WaitGroup
	wg.Add(writers)
	for range writers {
		go func() {
			defer wg.Done()
			for range perWriter {
				if _, err := writer.Write([]byte("retrying request\n")); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()

	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != writers*perWriter {
		t.Fatalf("got %d records, want %d -- records were merged or lost", len(lines), writers*perWriter)
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("unparsable record: %q", line)
		}
		if record["msg"] != "retrying request" {
			t.Fatalf("record content merged: %v", record)
		}
	}
}

// Regression for a review finding: WithContext used to reuse the parent's
// writer, so a library writing through Writer() during a traced request
// produced unstamped records while direct calls on the same adapter were
// stamped. Carrying the context is the whole reason this adapter exists over
// NewZap, so it has to hold for the writer too.
func TestSlogLoggerWriterInheritsTheAdapterContext(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)))

	if _, err := adapter.WithContext(sampledContext()).Writer().Write([]byte("GET /.well-known\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" {
		t.Fatalf("writer records must carry the adapter's trace id: %v", record)
	}
}

// The context must survive the field-adding path too, not just WithContext.
func TestSlogLoggerWriterKeepsContextAcrossWithField(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogLogger(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)))

	writer := adapter.WithContext(sampledContext()).WithField("request_id", "abc").Writer()
	if _, err := writer.Write([]byte("retrying\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" || record["request_id"] != "abc" {
		t.Fatalf("context or field lost on the writer: %v", record)
	}
}

// A writer with no context of its own still works and simply carries no ids.
func TestLineWriterWithoutContextEmitsUnstamped(t *testing.T) {
	var buf bytes.Buffer
	writer := NewLineWriter(NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)), slog.LevelInfo)

	if _, err := writer.Write([]byte("no span here\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, ok := decodeRecord(t, &buf)["trace_id"]; ok {
		t.Fatal("a writer with no span must not invent a trace id")
	}
}
