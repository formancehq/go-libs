package logging

import (
	"context"
	"os"
)

type contextKey string

var loggerKey contextKey = "_logger"

func FromContext(ctx context.Context) Logger {
	l := ctx.Value(loggerKey)
	if l == nil {
		// if a logger is not set in the context we initialize a new one without tracing hooks configured
		// this is mostly expected to happen in testing contexts as the app root should be propagating the logger when creating contexts
		return NewDefaultLogger(os.Stderr, false, false, false)
	}
	return l.(Logger)
}

func ContextWithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l.WithContext(ctx))
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
