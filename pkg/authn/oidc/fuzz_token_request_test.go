package oidc

import (
	"encoding/json"
	"testing"
)

func FuzzJWTTokenRequestUnmarshalJSON(f *testing.F) {
	f.Add([]byte(`{"iss":"issuer","sub":"subject","aud":"audience","iat":1609459200,"exp":1609459200}`))
	f.Add([]byte(`{"iss":"issuer","sub":"subject","aud":["aud1","aud2"],"iat":0,"exp":0}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"custom_claim":"value","iss":"test"}`))

	// Edge cases
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`"string"`))
	f.Add([]byte(`{`))
	f.Add([]byte(`{"iss":123}`))
	f.Add([]byte(`{"aud":42}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var req JWTTokenRequest
		err := req.UnmarshalJSON(data)
		if err != nil {
			return
		}

		// Round-trip
		marshaled, err := req.MarshalJSON()
		if err != nil {
			t.Fatalf("marshal failed after successful unmarshal: %v", err)
		}

		var reparsed JWTTokenRequest
		if err := reparsed.UnmarshalJSON(marshaled); err != nil {
			t.Fatalf("round-trip failed: %q -> %v", string(marshaled), err)
		}
	})
}

func FuzzMergeAndMarshalClaims(f *testing.F) {
	f.Add(`{"iss":"test","sub":"user"}`, `{"custom":"value"}`)
	f.Add(`{}`, `{}`)
	f.Add(`{"iss":"test"}`, `{"iss":"override"}`)
	f.Add(`{"a":"1"}`, ``)

	f.Fuzz(func(t *testing.T, registeredJSON string, extraJSON string) {
		var registered map[string]any
		if err := json.Unmarshal([]byte(registeredJSON), &registered); err != nil {
			return
		}

		var extra map[string]any
		if extraJSON != "" {
			if err := json.Unmarshal([]byte(extraJSON), &extra); err != nil {
				return
			}
		}

		// Must not panic
		result, err := mergeAndMarshalClaims(registered, extra)
		if err != nil {
			return
		}

		// Result must be valid JSON
		var check map[string]any
		if err := json.Unmarshal(result, &check); err != nil {
			t.Fatalf("mergeAndMarshalClaims produced invalid JSON: %q -> %v", string(result), err)
		}
	})
}
