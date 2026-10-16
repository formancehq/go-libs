package traces

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// RecordSpanError records an error and marks span failed, without ending it or
// adding an exception stacktrace. A nil error leaves the span unchanged.
func RecordSpanError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// AddLinks attaches valid origins once per trace/span identity to the active
// span. It does not start a span or retain the caller's context.
func AddLinks(ctx context.Context, origins ...trace.SpanContext) {
	span := trace.SpanFromContext(ctx)
	type identity struct {
		traceID trace.TraceID
		spanID  trace.SpanID
	}
	seen := make(map[identity]struct{}, len(origins))
	for _, origin := range origins {
		if !origin.IsValid() {
			continue
		}
		key := identity{origin.TraceID(), origin.SpanID()}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		span.AddLink(trace.Link{SpanContext: origin})
	}
}
