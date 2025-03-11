package api_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/formancehq/go-libs/v2/api"
	"github.com/stretchr/testify/require"
)

func TestQueryParamBool(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		queryParams    map[string]string
		key            string
		expectedResult bool
	}{
		{
			name: "true value",
			queryParams: map[string]string{
				"debug": "true",
			},
			key:            "debug",
			expectedResult: true,
		},
		{
			name: "1 value",
			queryParams: map[string]string{
				"debug": "1",
			},
			key:            "debug",
			expectedResult: true,
		},
		{
			name: "false value",
			queryParams: map[string]string{
				"debug": "false",
			},
			key:            "debug",
			expectedResult: false,
		},
		{
			name: "0 value",
			queryParams: map[string]string{
				"debug": "0",
			},
			key:            "debug",
			expectedResult: false,
		},
		{
			name: "empty value",
			queryParams: map[string]string{
				"debug": "",
			},
			key:            "debug",
			expectedResult: false,
		},
		{
			name:           "missing key",
			queryParams:    map[string]string{},
			key:            "debug",
			expectedResult: false,
		},
		{
			name: "case insensitive true",
			queryParams: map[string]string{
				"debug": "TRUE",
			},
			key:            "debug",
			expectedResult: true,
		},
		{
			name: "invalid value",
			queryParams: map[string]string{
				"debug": "yes",
			},
			key:            "debug",
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create a URL with query parameters
			u, _ := url.Parse("http://example.com")
			q := u.Query()
			for k, v := range tc.queryParams {
				q.Set(k, v)
			}
			u.RawQuery = q.Encode()

			// Create a request with the URL
			req, _ := http.NewRequest("GET", u.String(), nil)

			// Call the function
			result := api.QueryParamBool(req, tc.key)

			// Check the result
			require.Equal(t, tc.expectedResult, result)
		})
	}
}
