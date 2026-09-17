package logging

import (
	"bytes"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// The renaming lives in a core, so it has to hold on every façade over one
// logger -- not only the one that happened to implement it. Each of these
// emitted a duplicate key before.
func TestEveryFacadeEscapesReservedFields(t *testing.T) {
	for _, key := range reservedKeys {
		for _, tc := range []struct {
			facade string
			emit   func(*bytes.Buffer, string)
		}{
			{"slog", func(b *bytes.Buffer, k string) {
				NewSlog(NewZapLogger(b, zapcore.InfoLevel, true)).Info("actual message", k, "user value")
			}},
			{"logr", func(b *bytes.Buffer, k string) {
				NewLogr(NewZapLogger(b, zapcore.InfoLevel, true)).WithValues(k, "user value").Info("actual message")
			}},
			{"Logger", func(b *bytes.Buffer, k string) {
				NewZap(NewZapLogger(b, zapcore.InfoLevel, true).Sugar()).
					WithField(k, "user value").Infof("actual message")
			}},
			{"logrus opt-in", func(b *bytes.Buffer, k string) {
				sharedLogrus(b, InfoLevel, NewSharedJSONFormatter()).
					WithField(k, "user value").Infof("actual message")
			}},
		} {
			var buf bytes.Buffer
			tc.emit(&buf, key)

			if n := strings.Count(buf.String(), `"`+key+`":`); n > 1 {
				t.Fatalf("%s façade duplicates %q: %s", tc.facade, key, buf.String())
			}

			record := decodeRecord(t, &buf)
			if record["fields."+key] != "user value" {
				t.Fatalf("%s façade: the colliding field must survive under fields.%s: %v", tc.facade, key, record)
			}
			if record["msg"] != "actual message" {
				t.Fatalf("%s façade: the record's own message must win: %v", tc.facade, record)
			}
		}
	}
}

// A record carrying both "msg" and a literal "fields.msg" must not emit
// "fields.msg" twice and let argument order pick the winner.
func TestEscapedReservedFieldTakesPrecedence(t *testing.T) {
	for _, order := range [][]any{
		{"msg", "reserved", "fields.msg", "literal"},
		{"fields.msg", "literal", "msg", "reserved"},
	} {
		var buf bytes.Buffer
		NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).Info("actual message", order...)

		if n := strings.Count(buf.String(), `"fields.msg":`); n != 1 {
			t.Fatalf("fields.msg emitted %d times: %s", n, buf.String())
		}
		if got := decodeRecord(t, &buf)["fields.msg"]; got != "reserved" {
			t.Fatalf("fields.msg = %v, want the escaped reserved field to win whatever the order", got)
		}
	}

	// The shared logrus formatter has always resolved it this way; the two
	// must agree.
	var buf bytes.Buffer
	sharedLogrus(&buf, InfoLevel, NewSharedJSONFormatter()).
		WithField("msg", "reserved").WithField("fields.msg", "literal").Infof("actual message")

	if got := decodeRecord(t, &buf)["fields.msg"]; got != "reserved" {
		t.Fatalf("logrus: fields.msg = %v, want the escaped reserved field", got)
	}
}

// An application attribute must not shadow the span the record was emitted
// under: a consumer keeping the last value would read the user's string as the
// trace id.
func TestApplicationAttrsCannotShadowTraceCorrelation(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).
		InfoContext(sampledContext(), "x", "trace_id", "user value", "span_id", "user span")

	if n := strings.Count(buf.String(), `"trace_id":`); n != 1 {
		t.Fatalf("trace_id emitted %d times: %s", n, buf.String())
	}

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" {
		t.Fatalf("the active span must win: %v", record)
	}
	if record["fields.trace_id"] != "user value" || record["fields.span_id"] != "user span" {
		t.Fatalf("the application values must survive under fields.*: %v", record)
	}
}

// The escaping does not depend on a span being active, so a field's path is
// the same on a sampled and an unsampled record.
func TestTraceKeyEscapingDoesNotDependOnSampling(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).Info("x", "trace_id", "user value")

	record := decodeRecord(t, &buf)
	if record["fields.trace_id"] != "user value" {
		t.Fatalf("an untraced record must escape the same way: %v", record)
	}
}

// Grouped attributes are namespaced and must keep their own names.
func TestGroupedReservedNamesAreLeftAlone(t *testing.T) {
	var buf bytes.Buffer
	NewSlog(NewZapLogger(&buf, zapcore.InfoLevel, true)).
		WithGroup("request").Info("done", "msg", "inside", "trace_id", "inside")

	group, ok := decodeRecord(t, &buf)["request"].(map[string]any)
	if !ok {
		t.Fatalf("group missing: %s", buf.String())
	}
	if group["msg"] != "inside" || group["trace_id"] != "inside" {
		t.Fatalf("names inside a group must not be escaped: %v", group)
	}
}

// Field order follows the source. slog and zap carry a call order and keep it;
// logrus holds its fields in a map and has none, so the shared formatter sorts
// to stay deterministic. The two therefore agree only when the source order is
// the sorted one -- which is what the byte-identity tests construct, and what
// the documentation has to say rather than claim they can never disagree.
func TestFieldOrderFollowsTheSource(t *testing.T) {
	var fromZap bytes.Buffer
	NewSlog(NewZapLogger(&fromZap, zapcore.InfoLevel, true)).Info("x", "b", 2, "a", 1)

	if got := keyOrder(t, strings.TrimRight(fromZap.String(), "\n")); got != "level,time,msg,b,a" {
		t.Fatalf("the zap stack must keep the call order, got %q", got)
	}

	var fromLogrus bytes.Buffer
	sharedLogrus(&fromLogrus, InfoLevel, NewSharedJSONFormatter()).
		WithFields(map[string]any{"b": 2, "a": 1}).Infof("x")

	if got := keyOrder(t, strings.TrimRight(fromLogrus.String(), "\n")); got != "level,time,msg,a,b" {
		t.Fatalf("the logrus formatter must sort, having no order to preserve, got %q", got)
	}
}

func keyOrder(t *testing.T, line string) string {
	t.Helper()

	var keys []string
	for _, part := range strings.Split(line, `":`) {
		if i := strings.LastIndex(part, `"`); i >= 0 {
			keys = append(keys, part[i+1:])
		}
	}

	return strings.Join(keys, ",")
}

// Regression: a field scoped with With is encoded by the inner core, so a
// later Write never sees it. Without carrying the escaped keys forward, this
// emitted "fields.msg" twice.
func TestEscapedPrecedenceHoldsAcrossChainedWith(t *testing.T) {
	var buf bytes.Buffer
	z := NewZapLogger(&buf, zapcore.InfoLevel, true).With(zap.String("msg", "reserved"))
	z.Info("actual message", zap.String("fields.msg", "literal"))

	if n := strings.Count(buf.String(), `"fields.msg":`); n != 1 {
		t.Fatalf("fields.msg emitted %d times: %s", n, buf.String())
	}

	record := decodeRecord(t, &buf)
	if record["fields.msg"] != "reserved" {
		t.Fatalf("the escaped reserved field must win across With, got %v", record["fields.msg"])
	}
	if record["msg"] != "actual message" {
		t.Fatalf("the record's own message must win: %v", record)
	}
}

// Regression: a correlation id attached to the logger before NewSlog wraps it
// is retained by the core, so TraceHandler's escaping never sees it and the
// record carried the key twice -- a decoder keeping the first occurrence read
// the application's string as the trace id.
func TestScopedTraceFieldsCannotShadowCorrelation(t *testing.T) {
	var buf bytes.Buffer
	z := NewZapLogger(&buf, zapcore.InfoLevel, true).
		With(zap.String("trace_id", "user value"), zap.String("span_id", "user span"))

	NewSlog(z).InfoContext(sampledContext(), "x")

	if n := strings.Count(buf.String(), `"trace_id":`); n != 1 {
		t.Fatalf("trace_id emitted %d times: %s", n, buf.String())
	}

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" {
		t.Fatalf("the active span must win: %v", record)
	}
	if record["fields.trace_id"] != "user value" || record["fields.span_id"] != "user span" {
		t.Fatalf("the scoped values must survive under fields.*: %v", record)
	}
}

// Scoped reserved keys are escaped on every façade, not only under NewSlog.
func TestScopedReservedFieldsAreEscaped(t *testing.T) {
	var buf bytes.Buffer
	NewZapLogger(&buf, zapcore.InfoLevel, true).
		With(zap.String("msg", "user value")).Info("actual message")

	record := decodeRecord(t, &buf)
	if record["msg"] != "actual message" || record["fields.msg"] != "user value" {
		t.Fatalf("scoped reserved key not escaped: %v", record)
	}
}
