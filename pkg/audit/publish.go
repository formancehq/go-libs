package audit

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/formancehq/go-libs/v5/pkg/authn/jwt"
	"github.com/formancehq/go-libs/v5/pkg/messaging/publish"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// PublishEvent publishes an audit event to the configured publisher.
func PublishEvent(ctx context.Context, publisher message.Publisher, topicName string, appName string, payload Payload) {
	if err := publisher.Publish(
		topicName,
		publish.NewMessage(
			ctx,
			publish.EventMessage{
				Date:    time.Now().UTC(),
				App:     appName,
				Version: EventVersion,
				Type:    EventTypeAudit,
				Payload: payload,
			},
		),
	); err != nil {
		logging.FromContext(ctx).Errorf("failed to publish audit message: %v", err)
	}
}

// NewPayloadID generates a unique ID for an audit payload.
func NewPayloadID() string {
	return uuid.New().String()
}

// ExtractTraceID extracts the OpenTelemetry trace ID from the request context.
func ExtractTraceID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// ExtractIPAddress extracts the client IP address from an HTTP request.
// Priority: X-Forwarded-For > X-Real-IP > RemoteAddr.
func ExtractIPAddress(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip != "" {
		return ip
	}
	return r.RemoteAddr
}

// ExtractClaims extracts JWT claims from an HTTP request using the provided key sets.
func ExtractClaims(r *http.Request, opts *Options) (actor Actor) {
	actor.OrganizationID = opts.OrganizationID
	actor.StackID = opts.StackID
	actor.IPAddress = ExtractIPAddress(r)

	if opts.KeySets != nil {
		claims, tokenValidationError := jwt.ClaimsFromRequest(r, opts.KeySets)
		actor.Claims = claims
		actor.TokenValidationError = FormatTokenError(tokenValidationError)
	}

	return actor
}

// FormatTokenError returns an empty string for nil errors and expected
// "no authorization header" errors, otherwise the error message.
func FormatTokenError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, jwt.ErrNoAuthorizationHeader) {
		return ""
	}
	return err.Error()
}
