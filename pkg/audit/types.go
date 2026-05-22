package audit

import (
	"net/http"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
)

const (
	EventVersion   = "v2"
	EventTypeAudit = "AUDIT"
)

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
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Host   string      `json:"host"`
	Header http.Header `json:"header"`
	Body   string      `json:"body,omitempty"`
}

type HTTPResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body,omitempty"`
}
