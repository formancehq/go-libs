package logging

import (
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"
)

var _ logrus.Hook = (*traceHook)(nil)

type traceHook struct{}

// Fire implements logrus.Hook.
func (h *traceHook) Fire(entry *logrus.Entry) error {
	ctx := entry.Context
	if ctx == nil {
		return nil
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return nil
	}

	entry.Data["trace_id"] = span.SpanContext().TraceID().String()
	entry.Data["span_id"] = span.SpanContext().SpanID().String()
	return nil

}

// Levels implements logrus.Hook.
func (h *traceHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
	}
}

func NewTraceHook() *traceHook {
	return &traceHook{}
}
