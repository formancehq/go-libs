package redact

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBytesMasksProviderCredentialSpellings(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"nested auth":      `{"data":{"channel":"private-trades","auth":"bitstamp-token"}}`,
		"escaped secret":   `{"client_secret":"before\\\"after"}`,
		"partial escape":   `{"api_key":"secret-ending-in-backslash\`,
		"camel case":       `{"secretAccessKey":"aws-secret"}`,
		"header echo":      "X-CB-ACCESS-KEY: coinbase-key\n",
		"credential id":    `{"api_key_id":"service-account-secret"}`,
		"signature":        `{"signature":"signed-auth-proof"}`,
		"bearer":           "Authorization: Bearer eyJabc.def.ghi",
		"truncated secret": `{"api_key":"secret-without-closing-quote`,
		"form field":       "grant_type=client_credentials&client_secret=form-secret",
	}
	secrets := []string{
		"bitstamp-token", `before\\\"after`, "secret-ending-in-backslash", "aws-secret", "coinbase-key",
		"service-account-secret", "signed-auth-proof", "eyJabc.def.ghi", "secret-without-closing-quote", "form-secret",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := string(Bytes([]byte(input)))
			require.Contains(t, got, Marker)
			for _, secret := range secrets {
				require.NotContains(t, got, secret)
			}
		})
	}
}

func TestBytesPreservesDomainTokenIdentifiers(t *testing.T) {
	t.Parallel()

	got := string(Bytes([]byte(`{"token_id":"1234","tokenId":"5678","api_key_id":"credential-id"}`)))
	require.Contains(t, got, `"token_id":"1234"`)
	require.Contains(t, got, `"tokenId":"5678"`)
	require.NotContains(t, got, "credential-id")
	require.Contains(t, got, Marker)
}

func TestBytesRedactsCompleteSensitiveHeaderLines(t *testing.T) {
	t.Parallel()

	input := "X-Auth: BITSTAMP api-key-secret\r\nCookie: session-secret\nX-Nonce: nonce-secret\nerror: denied\n"
	got := string(Bytes([]byte(input)))
	for _, secret := range []string{"api-key-secret", "session-secret", "nonce-secret"} {
		require.NotContains(t, got, secret)
	}
	require.Contains(t, got, "X-Auth: "+Marker+"\r\n")
	require.Contains(t, got, "Cookie: "+Marker+"\n")
	require.Contains(t, got, "error: denied\n")
}

func TestBytesDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	input := []byte("Authorization: Bearer abc\n{\"password\":\"hunter2\"}")
	original := append([]byte(nil), input...)
	got := Bytes(input)
	require.Equal(t, original, input)
	require.NotContains(t, string(got), "hunter2")
}

func TestReaderRedactsBeforeDisplayCut(t *testing.T) {
	t.Parallel()

	secret := strings.Repeat("S", 100)
	got := Reader(strings.NewReader(`{"api_key":"`+secret+`"}`), 40)
	require.NotContains(t, got, "SSSS")
	require.Contains(t, got, Marker)
}

func TestReaderRedactsSecretCutAtEscapeIntroducer(t *testing.T) {
	t.Parallel()

	input := `{"api_key":"secret-ending-in-backslash\more"}`
	cut := len(`{"api_key":"secret-ending-in-backslash\`)
	got := Reader(strings.NewReader(input), cut-1)
	require.NotContains(t, got, "secret-ending")
	require.Contains(t, got, Marker)
}

func TestReaderBoundsReadAndOutput(t *testing.T) {
	t.Parallel()

	source := &countingReader{remaining: 1 << 20}
	got := Reader(source, 64)
	require.LessOrEqual(t, source.read, 65, "Reader must not consume more than maxBytes+1")
	require.True(t, strings.HasSuffix(got, "...[truncated]"), got)
	require.LessOrEqual(t, len(got), 64+len("...[truncated]"))

	short := Reader(strings.NewReader("  short body \n"), 64)
	require.Equal(t, "short body", short)

	defaulted := Reader(strings.NewReader(strings.Repeat("z", 5000)), 0)
	require.Equal(t, 4096, strings.Count(defaulted, "z"))
	require.True(t, strings.HasSuffix(defaulted, "...[truncated]"))
}

// countingReader yields 'x' bytes and records how many were consumed.
type countingReader struct {
	remaining int
	read      int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	for i := range n {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n

	return n, nil
}

func TestHeaderClassification(t *testing.T) {
	t.Parallel()

	// Vendor credential headers seen in provider integrations, split by
	// whether the value is a credential. Siblings such as X-Auth-Version or
	// X-Cb-Access-Timestamp carry no secret and must stay readable.
	sensitive := []string{
		"Authorization", "authorization", "Proxy-Authorization", "Cookie", "Set-Cookie",
		"Auth", "X-Auth", "X-Authentication", "X-Authorization",
		"X-Api-Key", "Api-Key", "X-Auth-Signature", "X-Auth-Nonce", "X-Nonce",
		"X-Cb-Access-Key", "X-Cb-Access-Passphrase", "X-Cb-Access-Signature",
		"Client-Secret", "Secret-Access-Key", "Passphrase", "X-Fireblocks-Api-Key",
		"X-Ratelimit-Token", "X-Webhook-Signature", "X-Amz-Security-Token",
	}
	safe := []string{
		"Accept", "Content-Type", "Content-Length", "User-Agent", "Etag",
		"Retry-After", "Ratelimit-Remaining", "Ratelimit-Reset", "X-Ratelimit-Remaining",
		"X-Request-Id", "X-Auth-Timestamp", "X-Auth-Version", "X-Auth-Subaccount-Id",
		"X-Cb-Access-Timestamp", "X-Consumer-Id", "X-Apideck-App-Id",
	}
	for _, name := range sensitive {
		require.True(t, SensitiveHeader(name), "%q should be sensitive", name)
	}
	for _, name := range safe {
		require.False(t, SensitiveHeader(name), "%q should remain visible", name)
	}
}

func TestHeadersMasksCredentialsAndKeepsDiagnostics(t *testing.T) {
	t.Parallel()

	got := Headers(http.Header{
		"Authorization":       {"Bearer secret"},
		"Content-Type":        {"application/json"},
		"Etag":                {`"v1"`},
		"Ratelimit-Remaining": {"42"},
		"X-Multi":             {"a", "b"},
	})
	require.Equal(t,
		`Authorization: [REDACTED]; Content-Type: application/json; Etag: "v1"; Ratelimit-Remaining: 42; X-Multi: a,b`,
		got)
	require.Empty(t, Headers(nil))
}

func TestURLMasksCredentialsAndKeepsUsefulRouting(t *testing.T) {
	t.Parallel()

	u, err := url.Parse("https://named-user:userinfo-secret@eth.g.alchemy.com/v2/alcht_0123456789abcdef/getAssetTransfers?api_key=query-secret&cursor=next")
	require.NoError(t, err)
	got := URL(u)
	for _, secret := range []string{"named-user", "userinfo-secret", "alcht_0123456789abcdef", "query-secret"} {
		require.NotContains(t, got, secret)
	}
	for _, useful := range []string{"eth.g.alchemy.com", "getAssetTransfers", "cursor=next"} {
		require.Contains(t, got, useful)
	}
}

func TestURLShapes(t *testing.T) {
	t.Parallel()

	const pathKey = "abcdef0123456789abcdef0123456789"
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "key in the path",
			raw:  "https://eth-mainnet.g.alchemy.com/v2/" + pathKey,
			want: "https://eth-mainnet.g.alchemy.com/v2/[REDACTED]",
		},
		{
			name: "key in the path, address query kept",
			raw:  "https://eth-mainnet.g.alchemy.com/nft/v3/" + pathKey + "/getContractMetadata?contractAddress=0xdac17f958d2ee523a2206206994597c13d831ec7",
			want: "https://eth-mainnet.g.alchemy.com/nft/v3/[REDACTED]/getContractMetadata?contractAddress=0xdac17f958d2ee523a2206206994597c13d831ec7",
		},
		{
			name: "credential query parameter masked by name",
			raw:  "https://api.example.com/v1/things?api_key=sk-live-secret&limit=10",
			want: "https://api.example.com/v1/things?api_key=%5BREDACTED%5D&limit=10",
		},
		{
			name: "access_token masked by name",
			raw:  "https://api.example.com/v1/things?access_token=tok-secret",
			want: "https://api.example.com/v1/things?access_token=%5BREDACTED%5D",
		},
		{
			name: "userinfo dropped entirely, not masked to user:xxxxx",
			raw:  "https://svcuser:svcpass@api.example.com/v1/things",
			want: "https://api.example.com/v1/things",
		},
		{
			name: "ordinary endpoint path untouched",
			raw:  "https://api.circle.com/v1/businessAccount/deposits",
			want: "https://api.circle.com/v1/businessAccount/deposits",
		},
		{
			name: "chain hash kept: it cannot be a credential",
			raw:  "https://eth-mainnet.g.alchemy.com/v2/" + pathKey + "/tx/0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b",
			want: "https://eth-mainnet.g.alchemy.com/v2/[REDACTED]/tx/0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b",
		},
		{
			name: "numeric id kept",
			raw:  "https://api.example.com/v1/accounts/1234567890123456789",
			want: "https://api.example.com/v1/accounts/1234567890123456789",
		},
		{
			name: "opaque resource id masked, which over-masks on purpose",
			raw:  "https://api.circle.com/v1/businessAccount/deposits/b8627ae4-4c4b-4b1a-8d4e-2f0e6a9a1c33",
			want: "https://api.circle.com/v1/businessAccount/deposits/[REDACTED]",
		},
		{
			name: "cursor stays readable: a parameter says what it is",
			raw:  "https://api.example.com/v1/things?cursor=eyJpZCI6ImFiYzEyMyJ9&pageSize=100",
			want: "https://api.example.com/v1/things?cursor=eyJpZCI6ImFiYzEyMyJ9&pageSize=100",
		},
		{
			name: "fragment dropped",
			raw:  "https://api.example.com/v1/things#access_token=fragment-secret",
			want: "https://api.example.com/v1/things",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tc.raw)
			require.NoError(t, err)
			got := URL(u)
			require.Equal(t, tc.want, got)
			require.NotContains(t, got, pathKey)
		})
	}
	require.Empty(t, URL(nil))
}

func TestURLMasksCloudCredentialQueries(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse("https://files.example/report?X-Amz-Credential=private-key&X-Amz-Signature=signed-value&X-Goog-Credential=google-key")
	require.NoError(t, err)
	secrets := []string{"private-key", "signed-value", "google-key"}

	rendered := URL(parsed)
	for _, secret := range secrets {
		require.NotContains(t, rendered, secret)
	}
	echoed := string(Bytes([]byte(parsed.String())))
	for _, secret := range secrets {
		require.NotContains(t, echoed, secret)
	}
}

func TestRequestURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty path", "https://api.example.com", "/"},
		{"origin form without scheme, host or userinfo", "https://svcuser:svcpass@api.example.com/v1/things?limit=10", "/v1/things?limit=10"},
		{"path and query masked", "https://api.example.com/v2/alcht_0123456789abcdef/call?token=tok-secret&cursor=next", "/v2/[REDACTED]/call?cursor=next&token=%5BREDACTED%5D"},
		{"query without path", "https://api.example.com?apiKey=query-secret", "/?apiKey=%5BREDACTED%5D"},
		{"escaped path kept escaped", "https://api.example.com/files/a%2Fb", "/files/a%2Fb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.want, RequestURI(u))
		})
	}

	opaque := &url.URL{Scheme: "https", Host: "api.example.com", Opaque: "/v2/alcht_0123456789abcdef/call", RawQuery: "limit=1"}
	require.Equal(t, "/v2/[REDACTED]/call?limit=1", RequestURI(opaque))
	require.Equal(t, "/", RequestURI(nil))
}

func TestPathKeepsShortAndPrefixedSegments(t *testing.T) {
	t.Parallel()

	require.Empty(t, Path(""))
	require.Equal(t, "/v2/page1/abcdefghijklmnopqrstuvwxyz", Path("/v2/page1/abcdefghijklmnopqrstuvwxyz"))
	require.Equal(t, "/0XABCDEF0123456789ABCDEF/[REDACTED]", Path("/0XABCDEF0123456789ABCDEF/abc0123456789def0"))
}

func TestHeadersRedactsURLValuedHeaders(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set("Location", "https://bucket.s3.amazonaws.com/report.csv?X-Amz-Signature=presigned-secret&X-Amz-Expires=300")
	h.Set("Content-Location", "https://user:pass@example.test/v1/items?page=2")
	h.Set("Referer", "%%not-a-url")

	got := Headers(h)
	for _, secret := range []string{"presigned-secret", "user:pass", "%%not-a-url"} {
		if strings.Contains(got, secret) {
			t.Errorf("Headers leaked %q: %s", secret, got)
		}
	}
	for _, kept := range []string{"X-Amz-Expires=300", "page=2", "/v1/items", "bucket.s3.amazonaws.com"} {
		if !strings.Contains(got, kept) {
			t.Errorf("Headers dropped %q: %s", kept, got)
		}
	}
}

// TestReaderCapsRedactionExpansion: a body shorter than the cap can grow past
// it once a short secret becomes the longer Marker; the preview cap must hold
// for the rendered text, not just the input.
func TestReaderCapsRedactionExpansion(t *testing.T) {
	t.Parallel()

	got := Reader(strings.NewReader("token=x"), 7)
	preview, truncated := strings.CutSuffix(got, "...[truncated]")
	require.True(t, truncated, "want the expansion marked as truncated: %q", got)
	require.LessOrEqual(t, len(preview), 7)
	require.NotContains(t, preview, "x")

	require.Equal(t, "ok", Reader(strings.NewReader("ok"), 7), "a body that fits is left whole")
}

func TestHeaderValuesSharesThePolicy(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{Marker}, HeaderValues("Authorization", []string{"Bearer a", "Bearer b"}))
	require.Equal(t, []string{"application/json"}, HeaderValues("Content-Type", []string{"application/json"}))
	got := HeaderValues("Location", []string{"https://h.example/p?X-Amz-Signature=sig-secret", "%%bad"})
	require.Len(t, got, 2)
	require.NotContains(t, got[0], "sig-secret")
	require.Equal(t, Marker, got[1], "an unparsable URL is masked whole")
}

func TestBytesMasksBareJSONCredentialValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{`{"password":123456,"ok":true}`, `{"password":[REDACTED],"ok":true}`},
		{`{"user_passcode": 4321 , "step":2}`, `{"user_passcode": [REDACTED] , "step":2}`},
		{`{"api_key":true,"n":1}`, `{"api_key":[REDACTED],"n":1}`},
		// Not credentials: identifiers, amounts, and nested objects stay.
		{`{"token_id":7,"amount":100}`, `{"token_id":7,"amount":100}`},
		{`{"credentials":{"id":3}}`, `{"credentials":{"id":3}}`},
	} {
		require.Equal(t, tc.want, string(Bytes([]byte(tc.in))), "Bytes(%s)", tc.in)
	}
}

// TestBytesMasksTheWholeBearerToken: slash, plus, tilde and "=" padding are
// bearer-token characters (RFC 6750 b64token), so a token carrying them must
// be masked to its end, not up to the first one.
func TestBytesMasksTheWholeBearerToken(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		`{"message":"Bearer abc/secret+tail="}`,
		`{"message":"bearer abc~secret/more+tail==","n":1}`,
		"upstream said: Bearer abc/secret+tail= then gave up",
		// JSON serializers may escape "/" as "\/" and any character as \uXXXX.
		`{"message":"Bearer abc\/secret+tail="}`,
		`{"message":"Bearer abc\u002fsecret\u002btail\u003d","n":1}`,
	} {
		got := string(Bytes([]byte(in)))
		for _, leak := range []string{"secret", "tail", "more"} {
			require.NotContains(t, got, leak, "Bytes(%q) = %q", in, got)
		}
		require.Contains(t, got, "[REDACTED]")
	}

	got := Reader(strings.NewReader(`{"message":"Bearer abc/secret+tail="}`), 4096)
	require.NotContains(t, got, "secret")
	require.Equal(t, `{"message":"Bearer [REDACTED]"}`, got)

	got = Reader(strings.NewReader(`{"message":"Bearer abc\/secret+tail="}`), 4096)
	require.Equal(t, `{"message":"Bearer [REDACTED]"}`, got)
}

// TestBytesClassifiesEscapedJSONFieldNames: JSON lets a field name spell any
// character as an escape, so {"password":...} is a password field and
// must be masked like one. The display shows the decoded name.
func TestBytesClassifiesEscapedJSONFieldNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{`{"\u0070assword":"hunter2","ok":true}`, `{"password":"[REDACTED]","ok":true}`},
		{`{"api\u005fkey" : "k-secret"}`, `{"api_key" : "[REDACTED]"}`},
		{`{"CLIENT\u005FSECRET":"cs-secret"}`, `{"CLIENT_SECRET":"[REDACTED]"}`},
		{`{"\u0070\u0061\u0073\u0073\u0063\u006f\u0064\u0065":987654,"n":1}`, `{"passcode":[REDACTED],"n":1}`},
		{`{"user\/name":"ann","pass\u0077ord":"\"quoted\" secret"}`, `{"user/name":"ann","password":"[REDACTED]"}`},
		// Not credentials: identifiers stay readable under their decoded name.
		{`{"token\u005fid":7,"amount":100}`, `{"token_id":7,"amount":100}`},
		// A name decoding to a quote or a control character keeps its spelling,
		// and an escaped value, quote or not, is not a field name.
		{`{"pass\"word":"x"}`, `{"pass\"word":"x"}`},
		{`{"a\u0000b":"x"}`, `{"a\u0000b":"x"}`},
		{`{"msg":"see \"password\": later"}`, `{"msg":"see \"password\": later"}`},
		{`{"path":"a\/b\u0070"}`, `{"path":"a\/b\u0070"}`},
	} {
		require.Equal(t, tc.want, string(Bytes([]byte(tc.in))), "Bytes(%s)", tc.in)
	}

	in := []byte(`{"\u0070assword":"hunter2"}`)
	orig := string(in)
	_ = Bytes(in)
	require.Equal(t, orig, string(in), "Bytes must not mutate its input")
}

func TestReaderClassifiesEscapedJSONFieldNames(t *testing.T) {
	t.Parallel()

	got := Reader(strings.NewReader(`{"\u0070assword":"hunter2","ok":true}`), 4096)
	require.Equal(t, `{"password":"[REDACTED]","ok":true}`, got)

	// The read window ends inside the secret: the dangling tail is masked too.
	secret := strings.Repeat("S", 100)
	got = Reader(strings.NewReader(`{"api\u005fkey":"`+secret+`"}`), 40)
	require.NotContains(t, got, "SSSS")
	require.Contains(t, got, Marker)
}
