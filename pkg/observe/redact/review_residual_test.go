package redact

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfiguredRawMatchesSurviveContextualNormalization(t *testing.T) {
	ConfigureValues([]string{"u0061longconfiguredcredential"})
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, test := range []struct{ input, want string }{
		{`detail=\u0061longconfiguredcredential`, `detail=[REDACTED]`},
		{`\u0062\u0063 detail=\u0061longconfiguredcredential`, `bc detail=[REDACTED]`},
		{`detail=\u0061longconfiguredcredential;next=\u0064`, `detail=[REDACTED];next=d`},
	} {
		require.Equal(t, test.want, string(Bytes([]byte(test.input))))
		require.Equal(t, test.want, Reader(strings.NewReader(test.input), 100))
	}
	require.Equal(t, `detail=[REDACTED]...[truncated]`, Reader(strings.NewReader(`detail=\u0061longconfiguredcredential`), 24))
}

func TestConfiguredOriginalOverlapAcrossDirectHelpers(t *testing.T) {
	ConfigureValues([]string{"first-known-prefix/shared-overlap", "shared-overlap/second-known-tail"})
	t.Cleanup(func() { ConfigureValues(nil) })
	input := "first-known-prefix/shared-overlap/second-known-tail"
	require.Equal(t, Marker, MaskValues(input))
	require.Equal(t, []string{Marker}, HeaderValues("X-Detail", []string{input}))
	require.NotContains(t, Headers(http.Header{"X-Detail": {input}}), "second-known-tail")
	encoded, ok := Form([]byte("note=" + url.QueryEscape(input)))
	require.True(t, ok)
	form, err := url.ParseQuery(string(encoded))
	require.NoError(t, err)
	require.Equal(t, Marker, form.Get("note"))
	original, err := url.Parse("https://example.test/?note=" + url.QueryEscape(input))
	require.NoError(t, err)
	parsed, err := url.Parse(URL(original))
	require.NoError(t, err)
	require.Equal(t, Marker, parsed.Query().Get("note"))
	require.NotContains(t, Path("/"+input), "second-known-tail")
	require.NotContains(t, RequestURI(&url.URL{Path: "/" + input}), "second-known-tail")
}

func TestConfiguredShortMatchesKeepOriginalBoundaries(t *testing.T) {
	ConfigureValues([]string{"abc", "def"})
	t.Cleanup(func() { ConfigureValues(nil) })
	require.Equal(t, "abc-def abcdef [REDACTED]/[REDACTED]", MaskValues("abc-def abcdef abc/def"))
}
