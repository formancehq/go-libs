package logging

import (
	"bytes"
	"context"
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
	for _, order := range [][]zap.Field{
		{zap.String("msg", "reserved"), zap.String("fields.msg", "literal")},
		{zap.String("fields.msg", "literal"), zap.String("msg", "reserved")},
	} {
		var buf bytes.Buffer
		NewZapLogger(&buf, zapcore.InfoLevel, true).Info("actual message", order...)

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
	NewZapWithTraces(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar()).
		WithContext(sampledContext()).
		WithField("trace_id", "user value").WithField("span_id", "user span").Infof("x")

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
	NewZapWithTraces(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar()).
		WithField("trace_id", "user value").Infof("x")

	record := decodeRecord(t, &buf)
	if record["fields.trace_id"] != "user value" {
		t.Fatalf("an untraced record must escape the same way: %v", record)
	}
}

// Field order follows the source. zap carries a call order and keeps it;
// logrus holds its fields in a map and has none, so the shared formatter sorts
// to stay deterministic. The two therefore agree only when the source order is
// the sorted one -- which is what the byte-identity tests construct, and what
// the documentation has to say rather than claim they can never disagree.
func TestFieldOrderFollowsTheSource(t *testing.T) {
	var fromZap bytes.Buffer
	NewZap(NewZapLogger(&fromZap, zapcore.InfoLevel, true).Sugar()).
		WithField("b", 2).WithField("a", 1).Infof("x")

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

// Regression: a correlation id attached to the logger before the Logger façade
// wraps it is retained by the core, so the record carried the key twice -- a
// decoder keeping the first occurrence read the application's string as the
// trace id.
func TestScopedTraceFieldsCannotShadowCorrelation(t *testing.T) {
	var buf bytes.Buffer
	z := NewZapLogger(&buf, zapcore.InfoLevel, true).
		With(zap.String("trace_id", "user value"), zap.String("span_id", "user span"))

	NewZapWithTraces(z.Sugar()).WithContext(sampledContext()).Infof("x")

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

// Scoped reserved keys are escaped on every façade, not only the correlating one.
func TestScopedReservedFieldsAreEscaped(t *testing.T) {
	var buf bytes.Buffer
	NewZapLogger(&buf, zapcore.InfoLevel, true).
		With(zap.String("msg", "user value")).Info("actual message")

	record := decodeRecord(t, &buf)
	if record["msg"] != "actual message" || record["fields.msg"] != "user value" {
		t.Fatalf("scoped reserved key not escaped: %v", record)
	}
}

// NewZap's contract is unchanged: no correlation, whatever the context. That is
// deliberate -- correlation is attached explicitly, as SetHooks does on the
// logrus side.
func TestNewZapDoesNotCorrelate(t *testing.T) {
	var buf bytes.Buffer
	NewZap(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar()).
		WithContext(sampledContext()).Infof("listening")

	if _, ok := decodeRecord(t, &buf)["trace_id"]; ok {
		t.Fatalf("NewZap must not correlate: %s", buf.String())
	}
}

// NewZapWithTraces is the opt-in counterpart.
func TestZapLoggerCorrelatesFromItsContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZapWithTraces(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar())

	logger.WithContext(sampledContext()).Infof("listening")

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" {
		t.Fatalf("trace_id missing: %v", record)
	}
	if record["span_id"] != "0200000000000000" {
		t.Fatalf("span_id missing: %v", record)
	}
}

// A context with no span adds nothing: an untraced record must not gain empty
// or invented correlation fields.
func TestZapLoggerAddsNothingWithoutASpan(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(Logger)
	}{
		{"no context at all", func(l Logger) { l.Infof("x") }},
		{"a context with no span", func(l Logger) { l.WithContext(context.Background()).Infof("x") }},
	} {
		var buf bytes.Buffer
		tc.emit(NewZapWithTraces(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar()))

		record := decodeRecord(t, &buf)
		if _, ok := record["trace_id"]; ok {
			t.Fatalf("%s: must not carry a trace id: %v", tc.name, record)
		}
	}
}

// The context survives the field-adding path, which is how the HTTP middleware
// composes a per-request logger.
func TestZapLoggerKeepsItsContextAcrossWithField(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZapWithTraces(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar())

	logger.WithContext(sampledContext()).WithField("request_id", "abc").Infof("Request")

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" || record["request_id"] != "abc" {
		t.Fatalf("context or field lost: %v", record)
	}
}

// ContextWithLogger is the path the middleware takes; it must produce a logger
// whose records are correlated.
func TestContextWithLoggerCorrelatesAZapLogger(t *testing.T) {
	var buf bytes.Buffer
	ctx := ContextWithLogger(sampledContext(), NewZapWithTraces(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar()))

	FromContext(ctx).Infof("Request")

	if got := decodeRecord(t, &buf)["trace_id"]; got != "01000000000000000000000000000000" {
		t.Fatalf("trace_id = %v", got)
	}
}

// The stamped pair must not be escaped as if it were an application field --
// the reason it is added on the Write path rather than through With.
func TestStampedCorrelationIsNotEscaped(t *testing.T) {
	var buf bytes.Buffer
	NewZapWithTraces(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar()).
		WithContext(sampledContext()).Infof("x")

	record := decodeRecord(t, &buf)
	if _, escaped := record["fields.trace_id"]; escaped {
		t.Fatalf("the stamped pair must not be escaped: %v", record)
	}
}

// And Trace survives, which is what this path offers over the slog bridge.
func TestZapLoggerKeepsTheTraceLevel(t *testing.T) {
	var buf bytes.Buffer
	NewZap(NewZapLogger(&buf, ToZapLevel(TraceLevel), true).Sugar()).Tracef("per-event detail")

	if got := decodeRecord(t, &buf)["level"]; got != "TRACE" {
		t.Fatalf("level = %v, want TRACE", got)
	}
}

// Regression: the escaping runs in a core, which sees fields after a namespace
// has been opened. Those are nested under it and cannot collide with a key
// written at the record root, so escaping them renamed a field for no reason
// -- and did it only on the façades that open a zap namespace.
func TestNamespacedFieldsAreNotEscaped(t *testing.T) {
	var buf bytes.Buffer
	NewZapLogger(&buf, zapcore.InfoLevel, true).With(
		zap.Namespace("request"),
		zap.String("msg", "inside"),
		zap.String("trace_id", "inside"),
	).Info("done")

	record := decodeRecord(t, &buf)
	if record["msg"] != "done" {
		t.Fatalf("the record's own message must win: %v", record)
	}

	group, ok := record["request"].(map[string]any)
	if !ok {
		t.Fatalf("the group must survive: %v", record)
	}
	if group["msg"] != "inside" || group["trace_id"] != "inside" {
		t.Fatalf("namespaced fields must keep their names: %v", group)
	}
}

// Regression: renameReserved drops a literal "fields.msg" that yields to an
// escaped "msg", so it can return fewer fields than it was given. With paired
// the two by index and read past the end -- a panic in a logging core, on a
// field name an application chooses, reachable from WithFields, zap's With and
// logr's WithValues.
func TestMixedReservedKeysInOneWithDoNotPanic(t *testing.T) {
	for _, order := range [][]any{
		{"msg", "reserved", "fields.msg", "literal"},
		{"fields.msg", "literal", "msg", "reserved"},
	} {
		var buf bytes.Buffer

		// A panic here fails the test on its own; the assertions below are what
		// pin the behaviour once it does not.
		NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar().
			With(order...).Info("actual message")

		if n := strings.Count(buf.String(), `"fields.msg":`); n != 1 {
			t.Fatalf("fields.msg emitted %d times: %s", n, buf.String())
		}

		record := decodeRecord(t, &buf)
		if record["fields.msg"] != "reserved" {
			t.Fatalf("the escaped reserved field must win whatever the order, got %v", record["fields.msg"])
		}
		if record["msg"] != "actual message" {
			t.Fatalf("the record's own message must win: %v", record)
		}
	}
}

// The same collision, scoped through the Logger façade rather than zap's own
// sugar, since that is the path application code takes.
func TestMixedReservedKeysThroughWithFieldsDoNotPanic(t *testing.T) {
	var buf bytes.Buffer

	NewZap(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar()).
		WithFields(map[string]any{"msg": "reserved", "fields.msg": "literal"}).
		Infof("actual message")

	if got := decodeRecord(t, &buf)["fields.msg"]; got != "reserved" {
		t.Fatalf("fields.msg = %v, want the escaped reserved field", got)
	}
}

// countingStringer records whether rendering it was ever attempted.
type countingStringer struct{ n *int }

func (c countingStringer) String() string { *c.n++; return "rendered" }

// Regression: a suppressed call must not pay for formatting. An earlier
// revision applied fmt.Sprintf before handing the record to zap, which charged
// every suppressed Debugf in a hot loop for a string nobody reads -- and made
// the Enabled guard documented on the interface a necessity rather than an
// optimisation.
func TestSuppressedRecordsAreNotFormatted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(*bytes.Buffer) Logger
	}{
		{"not correlating", func(b *bytes.Buffer) Logger {
			return NewZap(NewZapLogger(b, zapcore.InfoLevel, true).Sugar())
		}},
		{"correlating", func(b *bytes.Buffer) Logger {
			return NewZapWithTraces(NewZapLogger(b, zapcore.InfoLevel, true).Sugar())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := tc.build(&buf)

			renders := 0
			arg := countingStringer{n: &renders}

			logger.Debugf("%v", arg) // below the level
			logger.Debug(arg)        // below the level

			if renders != 0 {
				t.Fatalf("a suppressed record must not format its arguments, rendered %d time(s)", renders)
			}

			logger.Infof("%v", arg) // at the level
			if renders != 1 {
				t.Fatalf("an emitted record must format, rendered %d time(s)", renders)
			}
			if !strings.Contains(buf.String(), "rendered") {
				t.Fatalf("the formatted value must reach the record: %s", buf.String())
			}
		})
	}
}

// Regression: the correlation ids were escaped only on the With path, so a
// call site writing them on the record -- sugar.Infow, zapr's Info(msg, k, v)
// -- reached the encoder unescaped. On a correlating logger under an active
// span the record then carried trace_id twice, and a decoder keeping the first
// value read the application's string as the trace id.
func TestRecordLevelTraceKeysAreEscaped(t *testing.T) {
	var buf bytes.Buffer
	NewZapLogger(&buf, zapcore.InfoLevel, true).
		Info("x", zap.String("trace_id", "user value"), zap.String("span_id", "user span"))

	record := decodeRecord(t, &buf)
	if record["fields.trace_id"] != "user value" || record["fields.span_id"] != "user span" {
		t.Fatalf("record-level trace keys must be escaped like any application field: %v", record)
	}
	if _, unescaped := record["trace_id"]; unescaped {
		t.Fatalf("an application field must not sit at the correlation's key: %v", record)
	}
}

// And the pair the logger stamps must still reach the root, which is the whole
// reason the record path could not simply escape both.
func TestStampedPairSurvivesRecordLevelEscaping(t *testing.T) {
	var buf bytes.Buffer
	NewZapWithTraces(NewZapLogger(&buf, zapcore.InfoLevel, true).Sugar()).
		WithContext(sampledContext()).Infof("x")

	record := decodeRecord(t, &buf)
	if record["trace_id"] != "01000000000000000000000000000000" {
		t.Fatalf("the stamped pair must not be escaped: %v", record)
	}
	if _, escaped := record["fields.trace_id"]; escaped {
		t.Fatalf("the stamped pair must not be escaped: %v", record)
	}
}

// The shared logrus formatter escapes the same set as the zap stack, or a
// field's path changes when a service moves between them -- which is the
// property these formatters exist to provide.
func TestSharedFormatterEscapesTraceKeysLikeTheZapStack(t *testing.T) {
	for _, key := range []string{"trace_id", "span_id"} {
		var fromLogrus, fromZap bytes.Buffer

		sharedLogrus(&fromLogrus, InfoLevel, NewSharedJSONFormatter()).
			WithField(key, "user value").Infof("x")
		NewZap(NewZapLogger(&fromZap, zapcore.InfoLevel, true).Sugar()).
			WithField(key, "user value").Infof("x")

		if normalise(fromLogrus.String()) != normalise(fromZap.String()) {
			t.Fatalf("the stacks disagree on %q:\n logrus: %s\n    zap: %s",
				key, fromLogrus.String(), fromZap.String())
		}
		if decodeRecord(t, &fromLogrus)["fields."+key] != "user value" {
			t.Fatalf("logrus must escape %q like the zap stack", key)
		}
	}
}
