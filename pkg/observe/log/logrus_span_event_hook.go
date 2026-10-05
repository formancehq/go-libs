package logging

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// The keys below are the ones github.com/uptrace/opentelemetry-go-extra/otellogrus
// stamped (semconv v1.10.0), which this hook replaces. They are written as
// literals so that a semconv bump cannot silently rename the attributes of
// the span events services already query on.
const (
	logSeverityKey      = attribute.Key("log.severity")
	logMessageKey       = attribute.Key("log.message")
	codeFunctionKey     = attribute.Key("code.function")
	codeFilepathKey     = attribute.Key("code.filepath")
	codeLineNumberKey   = attribute.Key("code.lineno")
	exceptionTypeKey    = attribute.Key("exception.type")
	exceptionMessageKey = attribute.Key("exception.message")
)

var _ logrus.Hook = (*spanEventHook)(nil)

// spanEventHook records log entries as "log" events on the active span, and
// marks the span as errored for entries at errorStatusLevel or above.
type spanEventHook struct {
	levels           []logrus.Level
	errorStatusLevel logrus.Level
}

// Fire implements logrus.Hook.
func (h *spanEventHook) Fire(entry *logrus.Entry) error {
	ctx := entry.Context
	if ctx == nil {
		return nil
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return nil
	}

	attrs := make([]attribute.KeyValue, 0, len(entry.Data)+2+3)
	attrs = append(attrs,
		logSeverityKey.String(severityString(entry.Level)),
		logMessageKey.String(entry.Message),
	)

	if entry.Caller != nil {
		if entry.Caller.Function != "" {
			attrs = append(attrs, codeFunctionKey.String(entry.Caller.Function))
		}
		if entry.Caller.File != "" {
			attrs = append(attrs,
				codeFilepathKey.String(entry.Caller.File),
				codeLineNumberKey.Int(entry.Caller.Line),
			)
		}
	}

	for k, v := range entry.Data {
		if k == logrus.ErrorKey {
			if err, ok := v.(error); ok {
				attrs = append(attrs,
					exceptionTypeKey.String(reflect.TypeOf(err).String()),
					exceptionMessageKey.String(err.Error()),
				)
				continue
			}
		}
		attrs = append(attrs, spanAttribute(k, v))
	}

	span.AddEvent("log", trace.WithAttributes(attrs...))

	if entry.Level <= h.errorStatusLevel {
		span.SetStatus(codes.Error, entry.Message)
	}

	return nil
}

// Levels implements logrus.Hook.
func (h *spanEventHook) Levels() []logrus.Level {
	return h.levels
}

func NewSpanEventHook(levels ...logrus.Level) *spanEventHook {
	return &spanEventHook{
		levels:           levels,
		errorStatusLevel: logrus.ErrorLevel,
	}
}

func severityString(lvl logrus.Level) string {
	s := lvl.String()
	if s == "warning" {
		s = "warn"
	}
	return strings.ToUpper(s)
}

// spanAttribute converts a logrus field into a span attribute, keeping the
// value's type where OpenTelemetry has one for it.
func spanAttribute(key string, value any) attribute.KeyValue {
	switch value := value.(type) {
	case nil:
		return attribute.String(key, "<nil>")
	case string:
		return attribute.String(key, value)
	case int:
		return attribute.Int(key, value)
	case int64:
		return attribute.Int64(key, value)
	case uint64:
		return attribute.Int64(key, int64(value))
	case float64:
		return attribute.Float64(key, value)
	case bool:
		return attribute.Bool(key, value)
	case fmt.Stringer:
		return attribute.String(key, value.String())
	}

	rv := reflect.ValueOf(value)

	switch rv.Kind() {
	case reflect.Array:
		// reflect.ValueOf returns an unaddressable array, which Slice panics
		// on; copy it into an addressable one first.
		addressable := reflect.New(rv.Type()).Elem()
		addressable.Set(rv)
		rv = addressable.Slice(0, addressable.Len())
		fallthrough
	case reflect.Slice:
		// Converting rather than asserting keeps named slice types such as
		// `type Tags []string` from panicking inside the logging call.
		switch t := rv.Type(); {
		case t.ConvertibleTo(reflect.TypeFor[[]bool]()):
			return attribute.BoolSlice(key, rv.Convert(reflect.TypeFor[[]bool]()).Interface().([]bool))
		case t.ConvertibleTo(reflect.TypeFor[[]int]()):
			return attribute.IntSlice(key, rv.Convert(reflect.TypeFor[[]int]()).Interface().([]int))
		case t.ConvertibleTo(reflect.TypeFor[[]int64]()):
			return attribute.Int64Slice(key, rv.Convert(reflect.TypeFor[[]int64]()).Interface().([]int64))
		case t.ConvertibleTo(reflect.TypeFor[[]float64]()):
			return attribute.Float64Slice(key, rv.Convert(reflect.TypeFor[[]float64]()).Interface().([]float64))
		case t.ConvertibleTo(reflect.TypeFor[[]string]()):
			return attribute.StringSlice(key, rv.Convert(reflect.TypeFor[[]string]()).Interface().([]string))
		}
	case reflect.Bool:
		return attribute.Bool(key, rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return attribute.Int64(key, rv.Int())
	case reflect.Float64:
		return attribute.Float64(key, rv.Float())
	case reflect.String:
		return attribute.String(key, rv.String())
	}

	if b, err := json.Marshal(value); b != nil && err == nil {
		return attribute.String(key, string(b))
	}
	return attribute.String(key, fmt.Sprint(value))
}
