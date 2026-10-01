package webhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

const instrumentationName = "github.com/formancehq/go-libs/v5/pkg/transport/webhook"

// DefaultMaxBody is the body cap applied when [Config.MaxBody] is zero.
const DefaultMaxBody int64 = 1 << 20 // 1 MiB

// Verifier authenticates a delivery from its exact raw bytes before anything
// decodes it. A non-nil error rejects the delivery with 401.
//
// The request body has already been read when Verify runs: body holds every
// byte of it, and r.Body must not be read again. The receiver records only a
// fixed reason and the error's Go type, never its text (see [Config.Deliver]).
type Verifier interface {
	Verify(ctx context.Context, r *http.Request, body []byte) error
}

// VerifierFunc adapts a function to a [Verifier].
type VerifierFunc func(ctx context.Context, r *http.Request, body []byte) error

// Verify calls f(ctx, r, body).
func (f VerifierFunc) Verify(ctx context.Context, r *http.Request, body []byte) error {
	return f(ctx, r, body)
}

// Delivery is one verified webhook delivery. Deliver owns every field and may
// retain them after it returns.
type Delivery struct {
	// Header is a copy of the request headers.
	Header http.Header
	// Query is the parsed request query string.
	Query url.Values
	// Body is exactly the bytes that were verified.
	Body []byte
}

// Config configures a receiver built by [NewReceiver].
type Config struct {
	// MaxBody caps the request body in bytes. Zero applies [DefaultMaxBody];
	// a negative value is rejected by NewReceiver. A larger body is answered
	// with 413 and never truncated.
	MaxBody int64
	// Verifier authenticates every delivery before Deliver sees it. Required.
	Verifier Verifier
	// Deliver hands a verified delivery to the consumer. A non-nil error is
	// answered with 503 so the sender redelivers. Required.
	//
	// The error's text never reaches the span or the log: an error built from
	// the delivery can quote its body, headers or query. The receiver records
	// a fixed reason and the error's Go type; a consumer that wants the text
	// logs it inside Deliver, where it knows what is safe to keep.
	Deliver func(ctx context.Context, d Delivery) error
}

// NewReceiver returns an [http.Handler] that receives webhook deliveries as
// described in the package documentation.
func NewReceiver(cfg Config) (http.Handler, error) {
	if cfg.Verifier == nil {
		return nil, errors.New("webhook: Config.Verifier is required")
	}
	if cfg.Deliver == nil {
		return nil, errors.New("webhook: Config.Deliver is required")
	}
	if cfg.MaxBody < 0 {
		return nil, errors.New("webhook: Config.MaxBody must not be negative")
	}
	if cfg.MaxBody == 0 {
		cfg.MaxBody = DefaultMaxBody
	}

	return &receiver{
		maxBody:  cfg.MaxBody,
		verifier: cfg.Verifier,
		deliver:  cfg.Deliver,
	}, nil
}

type receiver struct {
	maxBody  int64
	verifier Verifier
	deliver  func(ctx context.Context, d Delivery) error
}

const (
	allowedMethods = http.MethodHead + ", " + http.MethodPost
	spanName       = "webhook.delivery"
)

// ServeHTTP implements [http.Handler]. HEAD and rejected methods never touch
// the request body; only a POST is a delivery and gets a span.
func (rc *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
	case http.MethodHead:
		w.WriteHeader(http.StatusOK)

		return
	default:
		w.Header().Set("Allow", allowedMethods)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	// Resolve the tracer per delivery so a provider installed after
	// NewReceiver, or swapped later, is honoured.
	ctx, span := otel.Tracer(instrumentationName).Start(r.Context(), spanName)
	defer span.End()

	status := rc.receive(ctx, span, w, r)
	span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status != http.StatusOK {
		http.Error(w, http.StatusText(status), status)

		return
	}
	w.WriteHeader(http.StatusOK)
}

// receive runs one POST delivery and returns the status to answer with. The
// order is fixed: cap, then verify, then deliver. Oversized bytes never reach
// the Verifier, and unverified bytes never reach Deliver.
func (rc *receiver) receive(ctx context.Context, span trace.Span, w http.ResponseWriter, r *http.Request) int {
	// A declared length past the cap is rejected before a single byte is read.
	if r.ContentLength > rc.maxBody {
		return reject(ctx, span, http.StatusRequestEntityTooLarge, "body exceeds the cap", nil)
	}

	body, err := rc.readBody(w, r)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return reject(ctx, span, http.StatusRequestEntityTooLarge, "body exceeds the cap", nil)
		}

		return reject(ctx, span, http.StatusBadRequest, "read body", err)
	}
	span.SetAttributes(attribute.Int("http.request.body.size", len(body)))

	// Neither callback's error text is recorded (see reject): it may describe
	// the signature expected or quote the delivery.
	if err := rc.verifier.Verify(ctx, r, body); err != nil {
		return reject(ctx, span, http.StatusUnauthorized, "verification failed", err)
	}

	err = rc.deliver(ctx, Delivery{
		Header: r.Header.Clone(),
		Query:  r.URL.Query(),
		Body:   body,
	})
	if err != nil {
		return reject(ctx, span, http.StatusServiceUnavailable, "deliver failed", err)
	}

	return http.StatusOK
}

// readBody reads the whole body, or fails with *http.MaxBytesError once it
// exceeds the cap. http.MaxBytesReader never asks the connection for more than
// maxBody+1 bytes, and on HTTP/1 it tells the server to close the connection
// instead of draining the rest.
func (rc *receiver) readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return []byte{}, nil
	}

	return io.ReadAll(http.MaxBytesReader(w, r.Body, rc.maxBody))
}

// reject marks the span failed with a fixed reason and logs the rejection at
// debug level. Neither carries the body, headers or query string, nor the
// error's text, which can quote any of them: only its Go type is kept, enough
// to tell a timeout from a full queue.
func reject(ctx context.Context, span trace.Span, status int, reason string, err error) int {
	span.SetStatus(codes.Error, reason)
	if err != nil {
		span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", err)))
	}

	logger := logging.FromContext(ctx)
	if logger.Enabled(logging.DebugLevel) {
		fields := map[string]any{
			"status": status,
			"reason": reason,
		}
		if err != nil {
			fields["error.type"] = fmt.Sprintf("%T", err)
		}
		logger.WithFields(fields).Debugf("webhook delivery rejected")
	}

	return status
}
