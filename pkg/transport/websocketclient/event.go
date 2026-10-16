package websocketclient

import "go.opentelemetry.io/otel/trace"

// Event is one queued value and its immutable asynchronous trace origin. It
// deliberately retains no context, cancellation or deadline. Only Value is
// charged against the encoded-payload budget.
type Event[T any] struct {
	Value  T
	Origin trace.SpanContext
}
