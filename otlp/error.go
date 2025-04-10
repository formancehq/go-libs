package otlp

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func RecordError(ctx context.Context, e error, opts ...trace.EventOption) {
	if e == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}
	span.SetStatus(codes.Error, e.Error())
	span.RecordError(e, append(opts, trace.WithStackTrace(true))...)
}

func RecordAsError(ctx context.Context, e any) {
	if e == nil {
		return
	}
	switch ee := e.(type) {
	case error:
		RecordError(ctx, ee)
	default:
		RecordError(ctx, fmt.Errorf("%s", e))
	}
}
