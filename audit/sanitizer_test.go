package audit_test

import (
	"net/http"
	"testing"

	"github.com/formancehq/go-libs/v3/audit"
)

// TestSanitizeHeaders tests the critical security feature of header sanitization
func TestSanitizeHeaders(t *testing.T) {
	tests := []struct {
		name             string
		input            http.Header
		sensitiveHeaders []string
		expectedPresent  []string
		expectedAbsent   []string
		testDeepCopy     bool
	}{
		{
			name: "should remove authorization header",
			input: http.Header{
				"Authorization": []string{"Bearer secret-token"},
				"Content-Type":  []string{"application/json"},
			},
			sensitiveHeaders: []string{"Authorization"},
			expectedPresent:  []string{"Content-Type"},
			expectedAbsent:   []string{"Authorization"},
		},
		{
			name: "should remove multiple sensitive headers",
			input: http.Header{
				"Authorization": []string{"Bearer token"},
				"Cookie":        []string{"session=abc123"},
				"X-Api-Key":     []string{"secret-key"},
				"Content-Type":  []string{"application/json"},
				"X-Request-Id":  []string{"req-123"},
			},
			sensitiveHeaders: []string{"Authorization", "Cookie", "X-Api-Key"},
			expectedPresent:  []string{"Content-Type", "X-Request-Id"},
			expectedAbsent:   []string{"Authorization", "Cookie", "X-Api-Key"},
		},
		{
			name: "should be case-insensitive for header names",
			input: http.Header{
				"authorization": []string{"Bearer token"},
				"COOKIE":        []string{"session=abc"},
				"Content-Type":  []string{"application/json"},
			},
			sensitiveHeaders: []string{"Authorization", "Cookie"},
			expectedPresent:  []string{"Content-Type"},
			expectedAbsent:   []string{"authorization", "COOKIE", "Authorization", "Cookie"},
		},
		{
			name: "should handle empty sensitive headers list",
			input: http.Header{
				"Authorization": []string{"Bearer token"},
				"Content-Type":  []string{"application/json"},
			},
			sensitiveHeaders: []string{},
			expectedPresent:  []string{"Authorization", "Content-Type"},
			expectedAbsent:   []string{},
		},
		{
			name: "should create deep copy (not affect original)",
			input: http.Header{
				"X-Custom": []string{"original-value"},
			},
			sensitiveHeaders: []string{},
			testDeepCopy:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of original for deep copy test
			originalCopy := make(http.Header)
			for k, v := range tt.input {
				originalCopy[k] = make([]string, len(v))
				copy(originalCopy[k], v)
			}

			// Sanitize headers
			sanitized := audit.SanitizeHeaders(tt.input, tt.sensitiveHeaders)

			// Check expected present headers
			for _, header := range tt.expectedPresent {
				if sanitized.Get(header) == "" {
					t.Errorf("expected header '%s' to be present but it was missing", header)
				}
			}

			// Check expected absent headers
			for _, header := range tt.expectedAbsent {
				if sanitized.Get(header) != "" {
					t.Errorf("expected header '%s' to be absent but it was present with value '%s'",
						header, sanitized.Get(header))
				}
			}

			// Test deep copy if requested
			if tt.testDeepCopy {
				// Modify the sanitized header
				sanitized.Set("X-Custom", "modified-value")

				// Original should remain unchanged
				if tt.input.Get("X-Custom") != originalCopy.Get("X-Custom") {
					t.Errorf("modifying sanitized header affected original (shallow copy issue)")
				}

				// Verify original still has original value
				if tt.input.Get("X-Custom") != "original-value" {
					t.Errorf("expected original header to remain 'original-value', got '%s'",
						tt.input.Get("X-Custom"))
				}
			}
		})
	}
}

// TestSanitizeHeadersWithMultipleValues tests headers with multiple values
func TestSanitizeHeadersWithMultipleValues(t *testing.T) {
	input := http.Header{
		"Set-Cookie": []string{
			"session=abc123; Secure; HttpOnly",
			"tracking=xyz789; SameSite=Lax",
		},
		"X-Custom": []string{"value1", "value2"},
	}

	sanitized := audit.SanitizeHeaders(input, []string{"Set-Cookie"})

	// Set-Cookie should be removed
	if len(sanitized.Values("Set-Cookie")) > 0 {
		t.Errorf("expected Set-Cookie to be removed, but found %d values", len(sanitized.Values("Set-Cookie")))
	}

	// X-Custom should still have all values
	customValues := sanitized.Values("X-Custom")
	if len(customValues) != 2 {
		t.Errorf("expected X-Custom to have 2 values, got %d", len(customValues))
	}
	if customValues[0] != "value1" || customValues[1] != "value2" {
		t.Errorf("expected X-Custom values to be preserved, got %v", customValues)
	}
}

// TestDefaultConfigSensitiveHeaders verifies default sensitive headers
func TestDefaultConfigSensitiveHeaders(t *testing.T) {
	cfg := audit.DefaultConfig("test")

	expectedSensitive := []string{
		"Authorization",
		"Cookie",
		"Set-Cookie",
		"X-API-Key",
		"X-Auth-Token",
		"Proxy-Authorization",
	}

	if len(cfg.SensitiveHeaders) != len(expectedSensitive) {
		t.Errorf("expected %d default sensitive headers, got %d",
			len(expectedSensitive), len(cfg.SensitiveHeaders))
	}

	// Create map for easy lookup
	sensitiveMap := make(map[string]bool)
	for _, h := range cfg.SensitiveHeaders {
		sensitiveMap[h] = true
	}

	// Verify all expected headers are present
	for _, expected := range expectedSensitive {
		if !sensitiveMap[expected] {
			t.Errorf("expected default sensitive header '%s' not found", expected)
		}
	}
}
