package httpclient

import (
	"net/url"

	"github.com/formancehq/go-libs/v5/pkg/observe/redact"
)

// diagnosticURL always uses configured-value masking as well as named-value
// masking. Credential-bearing paths require origin-only rendering.
func diagnosticURL(u *url.URL, pathIsCredential bool) string {
	if u == nil {
		return ""
	}
	if pathIsCredential {
		return redact.URL(&url.URL{Scheme: u.Scheme, Host: u.Host})
	}
	return redact.URL(u)
}
