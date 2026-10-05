package logging

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSpanEventHookRecordsLogAsSpanEvent(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	SetHooks(logger, true)

	type tags []string
	ctx, span := tracer.Start(context.Background(), "op")
	logger.WithContext(ctx).WithFields(logrus.Fields{
		"error": errors.New("boom"),
		"count": 3,
		"tags":  tags{"a", "b"},
	}).Error("failed")
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if got := spans[0].Status(); got.Code != codes.Error || got.Description != "failed" {
		t.Fatalf("status = %+v, want error \"failed\"", got)
	}

	events := spans[0].Events()
	if len(events) != 1 || events[0].Name != "log" {
		t.Fatalf("events = %+v, want one \"log\" event", events)
	}
	got := attribute.NewSet(events[0].Attributes...)
	for _, want := range []attribute.KeyValue{
		logSeverityKey.String("ERROR"),
		logMessageKey.String("failed"),
		exceptionTypeKey.String("*errors.errorString"),
		exceptionMessageKey.String("boom"),
		attribute.Int("count", 3),
		attribute.StringSlice("tags", []string{"a", "b"}),
	} {
		if v, ok := got.Value(want.Key); !ok || v != want.Value {
			t.Errorf("attribute %s = %v (present %v), want %v", want.Key, v.Emit(), ok, want.Value.Emit())
		}
	}
}

func TestSpanEventHookSkipsLevelsBelowWarn(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	SetHooks(logger, true)

	ctx, span := tracer.Start(context.Background(), "op")
	logger.WithContext(ctx).Info("hello")
	logger.WithContext(ctx).Warn("careful")
	span.End()

	s := recorder.Ended()[0]
	if n := len(s.Events()); n != 1 {
		t.Fatalf("events = %d, want 1 (warn only)", n)
	}
	if s.Status().Code == codes.Error {
		t.Fatalf("warn must not set error status")
	}
}
