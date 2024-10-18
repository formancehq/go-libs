package logging

import (
	"context"
	"os"

	"github.com/formancehq/go-libs/v2/otlp/otlptraces"
)

type contextKey string

var loggerKey contextKey = "_logger"

func FromContext(ctx context.Context) Logger {
	l := ctx.Value(loggerKey)
	if l == nil {
		var otelTraces string
		if flag := ctx.Value(otlptraces.OtelTracesExporterFlag); flag != nil {
			otelTraces = flag.(string)
		}
		return NewDefaultLogger(os.Stderr, false, false, otelTraces)
	}
	return l.(Logger)
}

func ContextWithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

func ContextWithFields(ctx context.Context, fields map[string]any) context.Context {
	return ContextWithLogger(ctx, FromContext(ctx).WithFields(fields))
}

func ContextWithField(ctx context.Context, key string, value any) context.Context {
	return ContextWithLogger(ctx, FromContext(ctx).WithFields(map[string]any{
		key: value,
	}))
}

func TestingContext() context.Context {
	return ContextWithLogger(context.Background(), Testing())
}
