package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalJSONWithStatusWritesStatusWithoutBodyForNilJSONValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		body   any
		status int
	}{
		{
			name:   "nil interface",
			body:   nil,
			status: stdhttp.StatusNoContent,
		},
		{
			name: "nil pointer",
			body: (*struct {
				ID string `json:"id"`
			})(nil),
			status: stdhttp.StatusAccepted,
		},
		{
			name:   "nil map",
			body:   map[string]string(nil),
			status: stdhttp.StatusCreated,
		},
		{
			name:   "nil slice",
			body:   []string(nil),
			status: stdhttp.StatusOK,
		},
		{
			name:   "nil channel",
			body:   (chan string)(nil),
			status: stdhttp.StatusOK,
		},
		{
			name:   "nil function",
			body:   (func())(nil),
			status: stdhttp.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()

			MarshalJSONWithStatus(recorder, tc.body, tc.status)

			require.Equal(t, tc.status, recorder.Code)
			require.Empty(t, recorder.Body.String())
			require.Equal(t, "application/json", recorder.Header().Get("content-type"))
		})
	}
}

func TestMarshalJSONWithStatusWritesBodyForNonNilJSONValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		body     any
		expected string
	}{
		{
			name: "struct pointer",
			body: &struct {
				ID string `json:"id"`
			}{ID: "test"},
			expected: `{"id":"test"}`,
		},
		{
			name:     "empty map",
			body:     map[string]string{},
			expected: `{}`,
		},
		{
			name:     "empty slice",
			body:     []string{},
			expected: `[]`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()

			MarshalJSONWithStatus(recorder, tc.body, stdhttp.StatusAccepted)

			require.Equal(t, stdhttp.StatusAccepted, recorder.Code)
			require.JSONEq(t, tc.expected, recorder.Body.String())
			require.Equal(t, "application/json", recorder.Header().Get("content-type"))
		})
	}
}
