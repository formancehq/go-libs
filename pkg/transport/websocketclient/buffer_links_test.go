package websocketclient

import (
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestKeyedDrainLinksOnlyReturnedValuesWithLatestOrigin(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	origin := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}})
	replacement := origin.WithSpanID(trace.SpanID{2})
	b := NewKeyedBuffer[string](3)
	b.Push("a", "old", origin)
	b.Push("b", "second", origin)
	b.Push("a", "new", replacement)
	ctx, span := provider.Tracer("test").Start(t.Context(), "bounded-drain")
	values, more := b.Drain(ctx, 1)
	span.End()
	if len(values) != 1 || values[0] != "new" || !more {
		t.Fatalf("values=%v more=%v", values, more)
	}
	links := recorder.Ended()[0].Links()
	if len(links) != 1 || !links[0].SpanContext.Equal(replacement) {
		t.Fatalf("links=%v", links)
	}
}
