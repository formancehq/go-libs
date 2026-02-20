package api_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v4/api"
)

func TestLink(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		link     api.Link
		expected string
	}{
		{
			name: "basic link",
			link: api.Link{
				Name: "test-link",
				URI:  "https://example.com/test",
			},
			expected: `{"name":"test-link","uri":"https://example.com/test"}`,
		},
		{
			name: "empty name",
			link: api.Link{
				Name: "",
				URI:  "https://example.com/test",
			},
			expected: `{"name":"","uri":"https://example.com/test"}`,
		},
		{
			name: "empty uri",
			link: api.Link{
				Name: "test-link",
				URI:  "",
			},
			expected: `{"name":"test-link","uri":""}`,
		},
		{
			name: "empty link",
			link: api.Link{
				Name: "",
				URI:  "",
			},
			expected: `{"name":"","uri":""}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Marshal the link to JSON
			data, err := json.Marshal(tc.link)
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(data))

			// Unmarshal back to verify
			var link api.Link
			err = json.Unmarshal(data, &link)
			require.NoError(t, err)
			require.Equal(t, tc.link, link)
		})
	}
}
