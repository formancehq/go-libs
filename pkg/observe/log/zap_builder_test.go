package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zapcore"
)

func sampledContext() context.Context {
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x1},
		SpanID:     trace.SpanID{0x2},
		TraceFlags: trace.FlagsSampled,
	}))
}

func decodeRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not valid JSON: %s (%v)", buf.String(), err)
	}

	return record
}

func TestParseZapLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want zapcore.Level
	}{
		{"trace", ToZapLevel(TraceLevel)},
		{"debug", zapcore.DebugLevel},
		{"info", zapcore.InfoLevel},
		{" ERROR ", zapcore.ErrorLevel},
		{"", zapcore.InfoLevel},
		{"nonsense", zapcore.InfoLevel},
	} {
		if got := ParseZapLevel(tc.in); got != tc.want {
			t.Fatalf("ParseZapLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Level has no Warn and rounds it away; zap has a real one.
func TestParseZapLevelHonoursWarn(t *testing.T) {
	for _, in := range []string{"warn", "warning", "WARN"} {
		if got := ParseZapLevel(in); got != zapcore.WarnLevel {
			t.Fatalf("ParseZapLevel(%q) = %v, want warn", in, got)
		}
	}
}

func TestZapLevelFromFlags(t *testing.T) {
	if got := ZapLevelFromFlags("error", true); got != zapcore.DebugLevel {
		t.Fatalf("--debug must clamp a less verbose --log-level to Debug, got %v", got)
	}
	if got := ZapLevelFromFlags("error", false); got != zapcore.ErrorLevel {
		t.Fatalf("without --debug the level stands, got %v", got)
	}
	if got := ZapLevelFromFlags("trace", true); got != ToZapLevel(TraceLevel) {
		t.Fatalf("--debug must not make an already more verbose level quieter, got %v", got)
	}
}

// The timestamp key is the field a deployment queries on; zap's default would
// put it under "ts" while every slog-based service writes "time".
func TestZapEncoderConfigRecordShape(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).Info("batch applied", "source", "aws")

	record := decodeRecord(t, &buf)
	if record["msg"] != "batch applied" {
		t.Fatalf("unexpected msg: %v", record)
	}
	if record["level"] != "INFO" {
		t.Fatalf("levels must be capitalised: %v", record)
	}
	if record["source"] != "aws" {
		t.Fatalf("attributes must reach the record: %v", record)
	}
	if _, ok := record["ts"]; ok {
		t.Fatalf("zap's default timestamp key must not survive: %v", record)
	}
	ts, ok := record["time"].(string)
	if !ok {
		t.Fatalf("record must carry a string \"time\": %v", record)
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Fatalf("time must be RFC3339 with nanoseconds, got %q: %v", ts, err)
	}
}

// slog encodes a duration as integer nanoseconds; zap's production default
// would emit float seconds and silently change every latency field.
func TestDurationsStayIntegerNanoseconds(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).Info("applied", "latency", 250*time.Millisecond)

	if got := decodeRecord(t, &buf)["latency"]; got != float64(250000000) {
		t.Fatalf("latency = %v, want 250000000", got)
	}
}

func TestNewSlogStampsTraceIDFromContext(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).InfoContext(sampledContext(), "source added")

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" {
		t.Fatalf("trace_id missing or wrong: %v", record)
	}
	if record["span_id"] != "0200000000000000" {
		t.Fatalf("span_id missing or wrong: %v", record)
	}
}

func TestNewSlogLeavesUntracedRecordsUnstamped(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).Info("provisioning")

	if _, ok := decodeRecord(t, &buf)["trace_id"]; ok {
		t.Fatal("a record emitted outside a span must not carry a trace id")
	}
}

// slog attaches no stack trace; zapslog would attach one to every Error record,
// which multiplies the size of the error lines a busy service emits.
func TestNewSlogDoesNotAttachStacktraces(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).Error("apply batch failed")

	if _, ok := decodeRecord(t, &buf)["stacktrace"]; ok {
		t.Fatalf("error records must not carry a stack trace: %s", buf.String())
	}
}
