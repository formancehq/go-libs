package traces

import (
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRecordSpanErrorDoesNotAddStacktraceOrEndSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	_, span := provider.Tracer("test").Start(t.Context(), "operation")
	RecordSpanError(span, nil)
	RecordSpanError(span, errors.New("operation failed"))
	if len(recorder.Ended()) != 0 {
		t.Fatal("helper ended caller-owned span")
	}
	span.End()
	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Status().Code != codes.Error || len(ended[0].Events()) != 1 {
		t.Fatalf("unexpected recorded error: %v", ended)
	}
	for _, attr := range ended[0].Events()[0].Attributes {
		if attr.Key == "exception.stacktrace" {
			t.Fatal("helper added a stacktrace")
		}
	}
}

func TestAddLinksDeduplicatesIdentityAndIgnoresInvalidOrigins(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	ctx, span := provider.Tracer("test").Start(t.Context(), "batch")
	origin := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled})
	changedFlags := origin.WithTraceFlags(0)
	second := origin.WithSpanID(trace.SpanID{2})
	AddLinks(ctx, trace.SpanContext{}, origin, changedFlags, second)
	span.End()
	links := recorder.Ended()[0].Links()
	if len(links) != 2 || !links[0].SpanContext.Equal(origin) || !links[1].SpanContext.Equal(second) {
		t.Fatalf("links=%v", links)
	}
}
