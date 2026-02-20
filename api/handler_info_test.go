package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v4/api"
)

func TestInfoHandler(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		info        api.ServiceInfo
		expectedRes api.ServiceInfo
	}{
		{
			name: "basic info",
			info: api.ServiceInfo{
				Version: "1.0.0",
				Debug:   false,
			},
			expectedRes: api.ServiceInfo{
				Version: "1.0.0",
				Debug:   false,
			},
		},
		{
			name: "debug mode",
			info: api.ServiceInfo{
				Version: "1.0.0",
				Debug:   true,
			},
			expectedRes: api.ServiceInfo{
				Version: "1.0.0",
				Debug:   true,
			},
		},
		{
			name: "empty version",
			info: api.ServiceInfo{
				Version: "",
				Debug:   false,
			},
			expectedRes: api.ServiceInfo{
				Version: "",
				Debug:   false,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create a request to pass to our handler
			req, err := http.NewRequest("GET", "/info", nil)
			require.NoError(t, err)

			// Create a ResponseRecorder to record the response
			rr := httptest.NewRecorder()
			handler := api.InfoHandler(tc.info)

			// Call the handler
			handler.ServeHTTP(rr, req)

			// Check the status code
			require.Equal(t, http.StatusOK, rr.Code)

			// Check the response body
			var response api.ServiceInfo
			err = json.NewDecoder(rr.Body).Decode(&response)
			require.NoError(t, err)
			require.Equal(t, tc.expectedRes, response)
		})
	}
}
