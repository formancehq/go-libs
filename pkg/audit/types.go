package audit

import (
	"net/http"
	"net/url"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
)

const (
	EventVersion   = "v2"
	EventTypeAudit = "AUDIT"
)

// HandledHeader is set by the audit middleware after processing a request.
// When present on an incoming request, the middleware skips audit to avoid
// duplicate events (e.g. gateway already audited, upstream service sees this header).
//
// Trust requirement: this header is only trustworthy when every hop that can
// set it is trusted. Edge components (e.g. the gateway) MUST strip it from
// external requests, or a shared secret MUST be configured (see
// httpaudit.WithHandledHeaderSecret / AuditHandledHeaderSecretFlag) so that
// only holders of the secret can mark a request as already audited.
// Otherwise, any external client can send this header to bypass the audit trail.
const HandledHeader = "X-Formance-Audit"

type Payload struct {
	ID      string `json:"id"`
	TraceID string `json:"trace_id"`
	Actor   Actor  `json:"actor"`
	HTTP    HTTP   `json:"http"`
	// GRPC describes a native gRPC call. It is nil for plain HTTP requests,
	// so their serialized form does not change.
	GRPC *GRPC `json:"grpc,omitempty"`
}

type Actor struct {
	Claims               *oidc.AccessTokenClaims `json:"claims,omitempty"`
	TokenValidationError string                  `json:"token_validation_error,omitempty"`
	OrganizationID       string                  `json:"organization_id"`
	StackID              string                  `json:"stack_id"`
	IPAddress            string                  `json:"ip_address"`
}

type HTTP struct {
	Request  HTTPRequest  `json:"request"`
	Response HTTPResponse `json:"response"`
}

type HTTPRequest struct {
	Method               string      `json:"method"`
	Path                 string      `json:"path"`
	QueryParams          url.Values  `json:"query_params,omitempty"`
	QueryParamsTruncated bool        `json:"query_params_truncated,omitempty"`
	Host                 string      `json:"host"`
	Header               http.Header `json:"header"`
	Body                 string      `json:"body,omitempty"`
	BodyTruncated        bool        `json:"body_truncated,omitempty"`
}

type HTTPResponse struct {
	StatusCode    int         `json:"status_code"`
	Headers       http.Header `json:"headers"`
	Body          string      `json:"body,omitempty"`
	BodyTruncated bool        `json:"body_truncated,omitempty"`
}

// GRPCTermination describes how a native gRPC call ended, as seen by the
// component that captured it.
type GRPCTermination string

const (
	// GRPCTerminationCompleted means the server sent a valid grpc-status.
	GRPCTerminationCompleted GRPCTermination = "completed"
	// GRPCTerminationCanceled means the client canceled the call.
	GRPCTerminationCanceled GRPCTermination = "canceled"
	// GRPCTerminationDeadlineExceeded means the call deadline expired.
	GRPCTerminationDeadlineExceeded GRPCTermination = "deadline_exceeded"
	// GRPCTerminationAborted means the handler aborted the stream.
	GRPCTerminationAborted GRPCTermination = "aborted"
	// GRPCTerminationGatewayError means an intermediate component returned an
	// error instead of forwarding the server response.
	GRPCTerminationGatewayError GRPCTermination = "gateway_error"
	// GRPCTerminationIncomplete means the call returned without a valid
	// grpc-status.
	GRPCTerminationIncomplete GRPCTermination = "incomplete"
)

// GRPCBodyEncodingBase64 is the encoding of GRPCBody.Data. The data holds the
// raw gRPC length-prefixed frames, not decoded protobuf messages.
const GRPCBodyEncodingBase64 = "base64"

// GRPC is the native gRPC extension of an audit payload.
type GRPC struct {
	Service string `json:"service"`
	Method  string `json:"method"`
	// StatusCode is the observed grpc-status (0 through 16). It is nil when
	// no valid status was observed.
	StatusCode    *int            `json:"status_code,omitempty"`
	StatusMessage string          `json:"status_message,omitempty"`
	Termination   GRPCTermination `json:"termination"`
	// GatewayStatusCode is the HTTP status proposed by an intermediate
	// component for its own error. It is separate from the observed status.
	GatewayStatusCode int      `json:"gateway_status_code,omitempty"`
	Request           GRPCBody `json:"request"`
	Response          GRPCBody `json:"response"`
}

// GRPCBody is a bounded capture of one direction of a gRPC stream.
type GRPCBody struct {
	Encoding   string `json:"encoding"`
	Data       []byte `json:"data,omitempty"`
	BytesTotal int64  `json:"bytes_total"`
	Truncated  bool   `json:"truncated"`
	Complete   bool   `json:"complete"`
}
