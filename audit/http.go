package audit

import "net/http"

// HTTPRequest captures HTTP request details
type HTTPRequest struct {
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Host   string      `json:"host"`
	Header http.Header `json:"header"`
	Body   string      `json:"body,omitempty"`
}

// HTTPResponse captures HTTP response details
type HTTPResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body,omitempty"`
}
