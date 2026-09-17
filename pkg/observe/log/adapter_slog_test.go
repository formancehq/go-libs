package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"

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

// Regression for a review finding: the name belongs to the *zap.Logger, not to
// the core zapslog receives, so a named logger used to lose it on the slog
// path while keeping it on the zap and logr paths -- two façades of one logger
// disagreeing about the shared record shape.
func TestNewSlogPreservesTheLoggerName(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true).Named("worker")).Info("batch applied")

	if got := decodeRecord(t, &buf)["logger"]; got != "worker" {
		t.Fatalf("logger name = %v, want worker", got)
	}
}

// The three façades of one named logger must agree.
func TestNamedLoggerRendersTheSameNameOnEveryFacade(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*bytes.Buffer)
	}{
		{"slog", func(b *bytes.Buffer) { NewSlog(NewZapLogger(b, zapcore.InfoLevel, true).Named("worker")).Info("x") }},
		{"logr", func(b *bytes.Buffer) { NewLogr(NewZapLogger(b, zapcore.InfoLevel, true).Named("worker")).Info("x") }},
		{"Logger", func(b *bytes.Buffer) {
			NewZap(NewZapLogger(b, zapcore.InfoLevel, true).Named("worker").Sugar()).Infof("x")
		}},
	} {
		var buf bytes.Buffer
		tc.emit(&buf)

		if got := decodeRecord(t, &buf)["logger"]; got != "worker" {
			t.Fatalf("%s façade: logger = %v, want worker", tc.name, got)
		}
	}
}

// An unnamed logger must not gain an empty field.
func TestUnnamedLoggerEmitsNoLoggerField(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).Info("x")

	if _, ok := decodeRecord(t, &buf)["logger"]; ok {
		t.Fatalf("an unnamed logger must not emit a logger key: %s", buf.String())
	}
}

// Regression for a review finding: the wrapped handler nests everything added
// after WithGroup, so delegating groups to it put the trace ids inside the
// caller's group -- nothing querying trace_id at the record root would find
// them.
func TestNewSlogKeepsTraceIDsAtTheRootUnderAGroup(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).
		WithGroup("request").
		InfoContext(sampledContext(), "done", "path", "/v1alpha1/connectors")

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" {
		t.Fatalf("trace_id must stay at the record root: %v", record)
	}
	if record["span_id"] != "0200000000000000" {
		t.Fatalf("span_id must stay at the record root: %v", record)
	}

	group, ok := record["request"].(map[string]any)
	if !ok {
		t.Fatalf("the caller's group must still be emitted: %v", record)
	}
	if group["path"] != "/v1alpha1/connectors" {
		t.Fatalf("the group must carry the record's attributes: %v", group)
	}
	if _, ok := group["trace_id"]; ok {
		t.Fatalf("trace_id must not be duplicated inside the group: %v", group)
	}
}

func TestNewSlogKeepsTraceIDsAtTheRootUnderNestedGroups(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).
		WithGroup("outer").With("a", 1).WithGroup("inner").
		InfoContext(sampledContext(), "done", "b", 2)

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" {
		t.Fatalf("trace_id must survive nested groups at the root: %v", record)
	}

	outer, ok := record["outer"].(map[string]any)
	if !ok {
		t.Fatalf("outer group missing: %v", record)
	}
	if outer["a"] != float64(1) {
		t.Fatalf("attribute added between the groups is misplaced: %v", outer)
	}
	inner, ok := outer["inner"].(map[string]any)
	if !ok {
		t.Fatalf("inner group missing: %v", outer)
	}
	if inner["b"] != float64(2) {
		t.Fatalf("record attribute is misplaced: %v", inner)
	}
}

// Reimplementing group handling is exactly the kind of change that breaks
// slog's contract in ways unit tests miss, so the standard library's own
// conformance suite runs against it.
func TestTraceHandlerSatisfiesSlogContract(t *testing.T) {
	var buf bytes.Buffer

	newHandler := func(*testing.T) slog.Handler {
		buf.Reset()

		return NewTraceHandler(slog.NewJSONHandler(&buf, nil))
	}

	result := func(t *testing.T) map[string]any {
		t.Helper()

		var record map[string]any
		if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
			t.Fatalf("output is not valid JSON: %s (%v)", buf.String(), err)
		}

		return record
	}

	slogtest.Run(t, newHandler, result)
}

// Regression for a review finding: the zap path passed a colliding attribute
// through unchanged, so the record carried two "msg" keys and a consumer
// keeping the last value lost the actual message. Both stacks rename now, so
// the shape holds whichever one a service logs through.
func TestNewSlogRenamesReservedFieldCollisions(t *testing.T) {
	for _, key := range []string{"msg", "level", "time", "logger"} {
		var buf bytes.Buffer
		NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).
			Info("actual message", key, "user value")

		// The encoder writes its own key at most once; "logger" is absent
		// entirely on an unnamed logger, which is why this is not an equality.
		line := buf.String()
		if strings.Count(line, `"`+key+`":`) > 1 {
			t.Fatalf("%q is duplicated: %s", key, line)
		}

		record := decodeRecord(t, &buf)
		if record["fields."+key] != "user value" {
			t.Fatalf("the colliding attribute must survive under fields.%s: %v", key, record)
		}
	}
}

func TestNewSlogLeavesGroupedAttributesAlone(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).
		WithGroup("request").Info("done", "msg", "inside a group")

	group, ok := decodeRecord(t, &buf)["request"].(map[string]any)
	if !ok {
		t.Fatalf("group missing: %s", buf.String())
	}
	if group["msg"] != "inside a group" {
		t.Fatalf("an attribute inside a group is namespaced and must keep its name: %v", group)
	}
}

// The two stacks must agree on this case, not merely each be sane.
func TestBothStacksRenameCollisionsIdentically(t *testing.T) {
	var fromZap, fromLogrus bytes.Buffer

	NewSlog(NewZapLogger(&fromZap, zapcore.InfoLevel, true)).Info("actual message", "msg", "user value")
	sharedLogrus(&fromLogrus, InfoLevel, NewSharedJSONFormatter()).
		WithField("msg", "user value").Infof("actual message")

	if normalise(fromZap.String()) != normalise(fromLogrus.String()) {
		t.Fatalf("the stacks diverge on a colliding field:\n   zap: %s\nlogrus: %s", fromZap.String(), fromLogrus.String())
	}
}

// The "logger" collision is only reachable on a named logger, where the
// encoder does write the key.
func TestNewSlogRenamesTheLoggerCollisionOnANamedLogger(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true).Named("worker")).
		Info("actual message", "logger", "user value")

	record := decodeRecord(t, &buf)
	if record["logger"] != "worker" {
		t.Fatalf("the logger's own name must win: %v", record)
	}
	if record["fields.logger"] != "user value" {
		t.Fatalf("the colliding attribute must survive under fields.logger: %v", record)
	}
}
