package otlp

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRecordError(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	err := errors.New("test error")
	RecordError(ctx, err)

	RecordError(ctx, err, trace.WithAttributes())
}

func TestRecordAsError(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	RecordAsError(ctx, errors.New("test error"))

	RecordAsError(ctx, "test error")

	RecordAsError(ctx, nil)
}
