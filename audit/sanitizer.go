package audit

import "net/http"

// SanitizeHeaders removes sensitive headers from the header map
func SanitizeHeaders(headers http.Header, sensitiveHeaders []string) http.Header {
	// Create a deep copy to avoid modifying original
	sanitized := headers.Clone()

	// Remove sensitive headers
	for _, header := range sensitiveHeaders {
		sanitized.Del(header)
	}

	return sanitized
}
