package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

const (
	// NOTE: this const is also copid inside the circuit breaker package
	// (to prevent a circular dependency). If you change it here, change it
	// there as well.
	otelContextKey = "otel-context"
)

// NewMessage preserves the legacy best-effort behavior. Prefer
// NewMessageWithError in code paths that can return marshal errors.
func NewMessage(ctx context.Context, m EventMessage) *message.Message {
	msg, err := NewMessageWithError(ctx, m)
	if err == nil {
		return msg
	}

	logging.FromContext(ctx).Errorf("failed to marshal event message: %v", err)
	m.Payload = nil
	msg, fallbackErr := NewMessageWithError(ctx, m)
	if fallbackErr == nil {
		return msg
	}

	logging.FromContext(ctx).Errorf("failed to marshal fallback event message: %v", fallbackErr)
	return newMessage(ctx, []byte("{}"))
}

func NewMessageWithError(ctx context.Context, m EventMessage) (*message.Message, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal event message: %w", err)
	}
	return newMessage(ctx, data), nil
}

func newMessage(ctx context.Context, data []byte) *message.Message {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	otelContext, _ := json.Marshal(carrier)

	msg := message.NewMessage(uuid.NewString(), data)
	msg.SetContext(ctx)
	msg.Metadata[otelContextKey] = string(otelContext)

	return msg
}

type EventMessage struct {
	IdempotencyKey string    `json:"idempotency_key"`
	Date           time.Time `json:"date"`
	App            string    `json:"app"`
	Version        string    `json:"version"`
	Type           string    `json:"type"`
	Payload        any       `json:"payload"`
}

func UnmarshalMessage(msg *message.Message) (trace.Span, *EventMessage, error) {
	ev := &EventMessage{}
	if err := json.Unmarshal(msg.Payload, ev); err != nil {
		return nil, nil, err
	}
	carrier := propagation.MapCarrier{}
	ctx := context.TODO()
	if err := json.Unmarshal([]byte(msg.Metadata[otelContextKey]), &carrier); err == nil {
		ctx = otel.GetTextMapPropagator().Extract(msg.Context(), carrier)
	}
	return trace.SpanFromContext(ctx), ev, nil
}
