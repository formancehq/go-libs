package audit

import "net/http"

// SanitizeHeaders removes sensitive headers from the header map
func SanitizeHeaders(headers http.Header, sensitiveHeaders []string) http.Header {
	// Create a copy to avoid modifying original
	sanitized := make(http.Header)
	for key, values := range headers {
		sanitized[key] = values
	}

	// Remove sensitive headers
	for _, header := range sensitiveHeaders {
		sanitized.Del(header)
	}

	return sanitized
}
