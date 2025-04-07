package otlp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRecordErrorOnRecover(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	t.Run("without panic", func(t *testing.T) {
		func() {
			defer RecordErrorOnRecover(ctx, false)()
		}()
	})

	t.Run("with panic but no forward", func(t *testing.T) {
		panicked := false
		func() {
			defer func() {
				r := recover()
				if r != nil {
					panicked = true
				}
			}()

			defer RecordErrorOnRecover(ctx, false)()
			panic("test panic")
		}()
		require.False(t, panicked, "La panique ne devrait pas être transmise")
	})

	t.Run("with panic and forward", func(t *testing.T) {
		panicked := false
		func() {
			defer func() {
				r := recover()
				if r != nil {
					panicked = true
					require.Equal(t, "test panic", r)
				}
			}()

			defer RecordErrorOnRecover(ctx, true)()
			panic("test panic")
		}()
		require.True(t, panicked, "La panique devrait être transmise")
	})
}
