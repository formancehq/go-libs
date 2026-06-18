package oidc

import (
	"encoding/json"
	"testing"
	stdtime "time"

	"github.com/stretchr/testify/require"

	libtime "github.com/formancehq/go-libs/v5/pkg/types/time"
)

func TestCheckNotBeforeUsesDecodedIDTokenClaimsNotBefore(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(map[string]int64{
		"nbf": stdtime.Now().Add(stdtime.Hour).Unix(),
	})
	require.NoError(t, err)

	var claims IDTokenClaims
	require.NoError(t, json.Unmarshal(payload, &claims))
	require.True(t, claims.TokenClaims.NotBefore.AsTime().IsZero())
	require.False(t, claims.NotBefore.AsTime().IsZero())

	require.ErrorIs(t, CheckNotBefore(&claims, 0), ErrNotBefore)
}

func TestCheckNotBeforePayloadReturnsMalformedNotBeforeError(t *testing.T) {
	t.Parallel()

	err := CheckNotBeforePayload([]byte(`{"nbf":"not-a-time"}`), 0)

	require.ErrorContains(t, err, "oidc.Time")
}

func TestCheckNotBeforeDoesNotRoundCurrentTimeForward(t *testing.T) {
	t.Parallel()

	now := libtime.New(stdtime.Unix(100, int64(600*stdtime.Millisecond)))
	notBefore := libtime.New(stdtime.Unix(101, 0))

	err := checkNotBeforeAt(testNotBeforeClaims{notBefore: notBefore}, 0, now)

	require.ErrorIs(t, err, ErrNotBefore)
}

type testNotBeforeClaims struct {
	notBefore libtime.Time
}

func (c testNotBeforeClaims) GetNotBefore() libtime.Time {
	return c.notBefore
}
