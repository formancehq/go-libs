package auth_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v3/auth"
	"github.com/formancehq/go-libs/v3/oidc"
)

func TestCheckAudienceClaim(t *testing.T) {
	tests := map[string]struct {
		expectedAudienceStr string
		claims              *oidc.AccessTokenClaims
		expectedError       error
	}{
		"NilClaims": {
			claims:        nil,
			expectedError: errors.New("claims cannot be nil"),
		},
		"MatchingAudience with url scheme": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"http://example.com"},
				},
			},
			expectedError: nil,
		},
		"NonMatchingAudience with url scheme": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"http://another.com"},
				},
			},
			expectedError: oidc.ErrAudience,
		},
		"MatchingAudience without url scheme": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"example.com"},
				},
			},
			expectedError: nil,
		},
		"NonMatchingAudience without url scheme": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"another.com"},
				},
			},
			expectedError: oidc.ErrAudience,
		},
		"Multiple audiences in claim; one matches": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"otherdomain.com", "example.com", "123.com"},
				},
			},
			expectedError: nil,
		},
		"Multiple audiences in claim but none match": {
			expectedAudienceStr: "http://example.com",
			claims: &oidc.AccessTokenClaims{
				TokenClaims: oidc.TokenClaims{
					Audience: []string{"another.com", "ple.com", "subdomain.example.com"},
				},
			},
			expectedError: oidc.ErrAudience,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			expectedAudience, err := url.Parse(tt.expectedAudienceStr)
			require.NoError(t, err)

			check := auth.CheckAudienceClaim(*expectedAudience)
			err = check(nil, tt.claims)
			assert.Equal(t, tt.expectedError, err)
		})
	}
}
