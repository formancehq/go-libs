package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v3/api"
)

func TestIdempotencyKeyFromRequest(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		headers        map[string]string
		expectedResult string
	}{
		{
			name: "with idempotency key",
			headers: map[string]string{
				"Idempotency-Key": "test-key-123",
			},
			expectedResult: "test-key-123",
		},
		{
			name:           "without idempotency key",
			headers:        map[string]string{},
			expectedResult: "",
		},
		{
			name: "with empty idempotency key",
			headers: map[string]string{
				"Idempotency-Key": "",
			},
			expectedResult: "",
		},
		{
			name: "case insensitive header",
			headers: map[string]string{
				"idempotency-key": "test-key-123",
			},
			expectedResult: "test-key-123",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create a request with the specified headers
			req, _ := http.NewRequest("GET", "/test", nil)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}

			// Call the function
			result := api.IdempotencyKeyFromRequest(req)

			// Check the result
			require.Equal(t, tc.expectedResult, result)
		})
	}
}
