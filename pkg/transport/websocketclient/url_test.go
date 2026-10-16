package websocketclient

import "testing"

func TestURLRewritesOnlyHTTPSchemes(t *testing.T) {
	for in, want := range map[string]string{
		"https://stream.example.test":       "wss://stream.example.test",
		"https://feed.example.test":         "wss://feed.example.test",
		"https://api.example.test/v2/route": "wss://api.example.test/v2/route",
		"http://127.0.0.1:8080":             "ws://127.0.0.1:8080",
		"http://localhost:8545/path?q=1":    "ws://localhost:8545/path?q=1",
		"wss://stream.example.test":         "wss://stream.example.test",
		"ws://127.0.0.1:8080":               "ws://127.0.0.1:8080",
		"HTTPS://upper.example":             "HTTPS://upper.example",
		"":                                  "",
	} {
		if got := URL(in); got != want {
			t.Errorf("URL(%q) = %q, want %q", in, got, want)
		}
	}
}
