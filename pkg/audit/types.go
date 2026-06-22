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
