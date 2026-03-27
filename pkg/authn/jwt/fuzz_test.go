package jwt

import (
	"net/http"
	"testing"

	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
)

func FuzzCheckScopes(f *testing.F) {
	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodDelete, http.MethodPatch, http.MethodOptions,
		http.MethodHead, http.MethodTrace,
	}

	// Seed with realistic inputs
	for _, method := range methods {
		f.Add("ledger", method, "ledger:read ledger:write")
		f.Add("ledger", method, "ledger:read")
		f.Add("ledger", method, "ledger:write")
		f.Add("ledger", method, "")
		f.Add("payments", method, "other:read")
	}

	f.Fuzz(func(t *testing.T, service string, method string, scopesStr string) {
		var scopes oidc.SpaceDelimitedArray
		if scopesStr != "" {
			_ = scopes.UnmarshalText([]byte(scopesStr))
		}

		// Must not panic
		valid, err := checkScopes(service, method, scopes)

		// Invariant: if valid is true, err must be nil
		if valid && err != nil {
			t.Error("valid=true but err is not nil")
		}
	})
}

func FuzzClaimsFromRequestHeader(f *testing.F) {
	// Focus on header parsing edge cases
	f.Add("Bearer eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJ0ZXN0In0.sig")
	f.Add("bearer eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJ0ZXN0In0.sig")
	f.Add("Bearer  token")
	f.Add("bearertoken")
	f.Add("Bearer\ttoken")
	f.Add("Bearer ")
	f.Add("bearer")
	f.Add("BEARER token")
	f.Add("Basic dXNlcjpwYXNz")
	f.Add("")
	f.Add("Bear token")
	f.Add("bearer ")

	f.Fuzz(func(t *testing.T, authHeader string) {
		r, _ := http.NewRequest(http.MethodGet, "/", nil)
		if authHeader != "" {
			r.Header.Set("Authorization", authHeader)
		}

		// Must not panic — errors are expected for most inputs
		_, _ = ClaimsFromRequest(r, nil)
	})
}
