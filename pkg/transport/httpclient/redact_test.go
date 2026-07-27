package httpclient

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRedactURL pins URL redaction against the shapes connectors actually send.
// url.URL.Redacted masks only the userinfo password, so a credential in the path
// or query survives it: Alchemy carries its API key as a path segment on every
// call, which is the case this exists for.
func TestRedactURL(t *testing.T) {
	t.Parallel()

	const alchemyKey = "abcdef0123456789abcdef0123456789"

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "alchemy json-rpc key in the path",
			raw:  "https://eth-mainnet.g.alchemy.com/v2/" + alchemyKey,
			want: "https://eth-mainnet.g.alchemy.com/v2/[REDACTED]",
		},
		{
			name: "alchemy nft key in the path, address kept",
			raw:  "https://eth-mainnet.g.alchemy.com/nft/v3/" + alchemyKey + "/getContractMetadata?contractAddress=0xdac17f958d2ee523a2206206994597c13d831ec7",
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
			name: "long endpoint names survive: masking those would gut the dump",
			raw:  "https://api.example.com/v1/positions_paginated/withdrawal-requests",
			want: "https://api.example.com/v1/positions_paginated/withdrawal-requests",
		},
		{
			name: "chain hash kept: it cannot be a credential and is what you need",
			raw:  "https://eth-mainnet.g.alchemy.com/v2/" + alchemyKey + "/tx/0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b",
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tc.raw)
			require.NoError(t, err)

			got := RedactURL(u)
			require.Equal(t, tc.want, got)
			// Whatever the rule, the key must never survive.
			require.NotContains(t, got, alchemyKey)
		})
	}
}

func TestRedactURLNil(t *testing.T) {
	t.Parallel()

	require.Empty(t, RedactURL(nil))
}

// TestSensitiveHeaderNames pins the classifier, since a miss here is a credential
// in a log file. The tables are the vendor spellings connectors actually set,
// split by whether the VALUE is a secret.
//
// X-Auth is the case that motivated grounding this in a real inventory rather than
// in plausible-looking examples: it reads like the harmless prefix of
// X-Auth-Timestamp, but Bitstamp sets it to "BITSTAMP <apiKey>", so the value IS
// the credential.
func TestSensitiveHeaderNames(t *testing.T) {
	t.Parallel()

	// Credential-bearing vendor spellings in use today.
	sensitive := []string{
		"Authorization",          // apideck, circle, formancepayments, fireblocks
		"X-Api-Key",              // fireblocks
		"X-API-Key",              // same header, vendor casing
		"X-Auth",                 // bitstamp: "BITSTAMP <apiKey>"
		"X-Auth-Signature",       // bitstamp HMAC
		"X-Auth-Nonce",           // bitstamp single-use nonce
		"X-Nonce",                // coinbaseprime
		"X-Cb-Access-Key",        // coinbaseprime
		"X-CB-ACCESS-KEY",        // same header, vendor casing
		"X-Cb-Access-Passphrase", //
		"X-Cb-Access-Signature",  //
		// Shapes not in use yet but cheap to keep covered, so a new connector
		// spelling does not arrive unmasked.
		"authorization", "Proxy-Authorization", "Cookie", "Set-Cookie",
		"Auth", "X-Authentication", "X-Authorization",
		"Api-Key", "Client-Secret", "Secret-Access-Key", "Passphrase",
		"X-Fireblocks-Api-Key", "X-Ratelimit-Token",
	}
	// Not secret, and a dump is worth less without them.
	safe := []string{
		"Accept",               // every connector
		"Content-Type",         // bitstamp, alchemy, circle, formancepayments
		"X-Auth-Timestamp",     // bitstamp: signed, not secret
		"X-Auth-Version",       // bitstamp
		"X-Auth-Subaccount-Id", // bitstamp: a scope, not a credential
		"X-Cb-Access-Timestamp",
		"X-Consumer-Id", // apideck per-call header
		"X-Apideck-App-Id",
		"Ratelimit-Remaining", "Ratelimit-Reset", "Retry-After",
		"X-Request-Id", "Etag", "Content-Length", "User-Agent",
	}

	for _, name := range sensitive {
		require.True(t, IsSensitiveHeader(name),
			"%q not classified as sensitive: its value would reach the log", name)
	}
	for _, name := range safe {
		require.False(t, IsSensitiveHeader(name),
			"%q wrongly classified as sensitive, which makes dumps less useful", name)
	}
}

// TestRedactHeadersShowsNonSensitiveValuesInFull pins the second half of the
// contract: masking credentials is worth nothing if it costs every other value.
func TestRedactHeadersShowsNonSensitiveValuesInFull(t *testing.T) {
	t.Parallel()

	got := RedactHeaders(http.Header{
		"Authorization":        {"Bearer super-secret-token"},
		"X-Auth":               {"BITSTAMP api-key-abc123"},
		"X-Auth-Version":       {"v2"},
		"Content-Type":         {"application/json"},
		"Etag":                 {`W/"686897696a7c876b7e"`},
		"Ratelimit-Remaining":  {"42"},
		"X-Multi":              {"one", "two"},
		"X-Auth-Subaccount-Id": {"sub-42"},
	})

	require.Equal(t, strings.Join([]string{
		"Authorization: [REDACTED]",
		`Content-Type: application/json`,
		`Etag: W/"686897696a7c876b7e"`,
		"Ratelimit-Remaining: 42",
		"X-Auth: [REDACTED]",
		"X-Auth-Subaccount-Id: sub-42",
		"X-Auth-Version: v2",
		"X-Multi: one,two",
	}, "; "), got)
	require.NotContains(t, got, "super-secret-token")
	require.NotContains(t, got, "api-key-abc123")
}

func TestRedactHeadersEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, RedactHeaders(nil))
}

// TestRedactBody covers the two things a body dump owes: it must not echo a
// credential the payload carries, and it must stop at the cap.
func TestRedactBody(t *testing.T) {
	t.Parallel()

	t.Run("masks secrets by key name", func(t *testing.T) {
		t.Parallel()

		got := RedactBody(strings.NewReader(
			`{"data":[{"id":"txn-1"}],"session_token":"leak-me-not","access_token":"also-not"}`,
		), DefaultMaxDumpBody)

		require.NotContains(t, got, "leak-me-not")
		require.NotContains(t, got, "also-not")
		// The useful part of the payload survives.
		require.Contains(t, got, "txn-1")
	})

	t.Run("masks a bearer token echoed in an error body", func(t *testing.T) {
		t.Parallel()

		got := RedactBody(strings.NewReader(`invalid header "Bearer abc.def-123"`), DefaultMaxDumpBody)
		require.NotContains(t, got, "abc.def-123")
	})

	t.Run("stops at the cap and says so", func(t *testing.T) {
		t.Parallel()

		got := RedactBody(strings.NewReader(strings.Repeat("y", DefaultMaxDumpBody*8)), DefaultMaxDumpBody)
		require.True(t, strings.HasSuffix(got, "...[truncated]"))
		require.Equal(t, DefaultMaxDumpBody, strings.Count(got, "y"))
	})

	t.Run("masks a secret the cap cut in half", func(t *testing.T) {
		t.Parallel()

		// The value runs past the read window, so its closing quote is never seen.
		body := `{"pad":"` + strings.Repeat("p", DefaultMaxDumpBody-24) + `","api_key":"` + strings.Repeat("s", 64) + `"}`
		got := RedactBody(strings.NewReader(body), DefaultMaxDumpBody)
		require.NotContains(t, got, strings.Repeat("s", 8))
	})

	t.Run("non-positive cap falls back to the default", func(t *testing.T) {
		t.Parallel()

		got := RedactBody(strings.NewReader(strings.Repeat("z", DefaultMaxDumpBody*2)), 0)
		require.Equal(t, DefaultMaxDumpBody, strings.Count(got, "z"))
	})
}
