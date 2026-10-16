package websocketclient

import "strings"

// URL converts an http(s) base URL to its ws(s) form; any other URL, a ws(s) one
// included, is returned unchanged. It lets a connector derive its live-tail URL
// from the http(s) base it already calls, and lets a test point a live tail at an
// httptest server while production configures a wss:// feed directly.
func URL(baseURL string) string {
	switch {
	case strings.HasPrefix(baseURL, "https://"):
		return "wss://" + strings.TrimPrefix(baseURL, "https://")
	case strings.HasPrefix(baseURL, "http://"):
		return "ws://" + strings.TrimPrefix(baseURL, "http://")
	default:
		return baseURL
	}
}
