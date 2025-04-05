package otlp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRecordErrorOnRecover(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	func() {
		defer RecordErrorOnRecover(ctx, false)()
	}()

	func() {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "test panic", r)
		}()
		
		defer RecordErrorOnRecover(ctx, false)()
		panic("test panic")
	}()

	func() {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "test panic", r)
		}()
		
		defer RecordErrorOnRecover(ctx, true)()
		panic("test panic")
	}()
}
