package licence

import (
	"testing"
)

func FuzzLicenceValidate(f *testing.F) {
	// Various JWT-like strings
	f.Add("eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJ0ZXN0Iiwic3ViIjoiY2x1c3RlciIsImF1ZCI6InNlcnZpY2UiLCJleHAiOjk5OTk5OTk5OTl9.invalid-sig")
	f.Add("not.a.jwt")
	f.Add("")
	f.Add("eyJhbGciOiJIUzI1NiJ9.eyJ0ZXN0IjoxfQ.sig")
	f.Add("a]b.c.d")
	f.Add("...")
	f.Add("eyJhbGciOiJub25lIn0.eyJ0ZXN0IjoxfQ.")

	f.Fuzz(func(t *testing.T, token string) {
		l := &Licence{
			jwtToken:       token,
			serviceName:    "test-service",
			clusterID:      "test-cluster",
			expectedIssuer: "test-issuer",
		}

		// Must not panic — errors are expected for invalid tokens
		_ = l.validate()
	})
}
