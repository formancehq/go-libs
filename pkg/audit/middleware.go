package audit

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/formancehq/go-libs/v5/pkg/authn/jwt"
	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/messaging/publish"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type options struct {
	keySets        map[string]oidc.KeySet
	organizationID string
	stackID        string
	sensitivePaths map[string]struct{}
}

type Option func(*options)

// WithAuth enables JWT claims extraction from the Authorization header.
func WithAuth(keySets map[string]oidc.KeySet) Option {
	return func(o *options) {
		o.keySets = keySets
	}
}

// WithOrganizationID sets the organization ID included in audit events.
func WithOrganizationID(id string) Option {
	return func(o *options) {
		o.organizationID = id
	}
}

// WithStackID sets the stack ID included in audit events.
func WithStackID(id string) Option {
	return func(o *options) {
		o.stackID = id
	}
}

// WithSensitivePaths sets paths for which the response body should not be captured.
func WithSensitivePaths(paths ...string) Option {
	return func(o *options) {
		for _, p := range paths {
			o.sensitivePaths[p] = struct{}{}
		}
	}
}

var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// Middleware returns an HTTP middleware that captures request/response data
// and publishes audit events via the given Watermill publisher.
func Middleware(publisher message.Publisher, topicName string, appName string, opts ...Option) func(http.Handler) http.Handler {
	o := &options{
		sensitivePaths: make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(o)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var (
				body []byte
				err  error
			)

			if !isStreamRequest(r) {
				body, err = io.ReadAll(r.Body)
				if err != nil && !errors.Is(err, io.EOF) {
					http.Error(w, "failed to read request body", http.StatusInternalServerError)
					return
				}
				if len(body) > 0 {
					_ = r.Body.Close()
					r.Body = io.NopCloser(bytes.NewBuffer(body))
				}
			}

			buf := bufPool.Get().(*bytes.Buffer)
			buf.Reset()
			defer bufPool.Put(buf)

			rww := &responseWriterWrapper{
				ResponseWriter: w,
				body:           buf,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(rww, r)

			responseBody := rww.body.String()
			if _, sensitive := o.sensitivePaths[r.URL.Path]; sensitive {
				responseBody = ""
			}

			var (
				claims               *oidc.AccessTokenClaims
				tokenValidationError error
			)
			if o.keySets != nil {
				claims, tokenValidationError = jwt.ClaimsFromRequest(r, o.keySets)
			}

			requestHeaders := r.Header.Clone()
			requestHeaders.Del("Authorization")

			payload := Payload{
				ID:      uuid.New().String(),
				TraceID: extractTraceID(r),
				Actor: Actor{
					Claims:               claims,
					TokenValidationError: formatTokenError(tokenValidationError),
					OrganizationID:       o.organizationID,
					StackID:              o.stackID,
					IPAddress:            extractIPAddress(r),
				},
				HTTP: HTTP{
					Request: HTTPRequest{
						Method: r.Method,
						Path:   r.URL.Path,
						Host:   r.Host,
						Header: requestHeaders,
						Body: func() string {
							if len(body) > 0 {
								return string(body)
							}
							return ""
						}(),
					},
					Response: HTTPResponse{
						StatusCode: rww.statusCode,
						Headers:    rww.Header(),
						Body:       responseBody,
					},
				},
			}

			if err := publisher.Publish(
				topicName,
				publish.NewMessage(
					r.Context(),
					publish.EventMessage{
						Date:    time.Now().UTC(),
						App:     appName,
						Version: EventVersion,
						Type:    EventTypeAudit,
						Payload: payload,
					},
				),
			); err != nil {
				logging.FromContext(r.Context()).Errorf("failed to publish audit message: %v", err)
			}
		})
	}
}

func isStreamRequest(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/vnd.formance") && strings.HasSuffix(ct, "-stream")
}

func formatTokenError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, jwt.ErrNoAuthorizationHeader) {
		return ""
	}
	return err.Error()
}

func extractIPAddress(r *http.Request) string {
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

func extractTraceID(r *http.Request) string {
	span := trace.SpanFromContext(r.Context())
	if span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

type responseWriterWrapper struct {
	http.ResponseWriter
	body           *bytes.Buffer
	statusCode     int
	headersFlushed bool
}

func (rww *responseWriterWrapper) Write(buf []byte) (int, error) {
	if rww.Header().Get("Content-Type") != "application/octet-stream" {
		rww.body.Write(buf)
	}
	return rww.ResponseWriter.Write(buf)
}

func (rww *responseWriterWrapper) WriteHeader(statusCode int) {
	rww.statusCode = statusCode
	rww.ResponseWriter.WriteHeader(statusCode)
}
