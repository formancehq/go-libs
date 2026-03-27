package oidc

import (
	"encoding/json"
	"testing"
)

func FuzzAudienceUnmarshalJSON(f *testing.F) {
	// Valid inputs
	f.Add([]byte(`"single-audience"`))
	f.Add([]byte(`["aud1", "aud2"]`))
	f.Add([]byte(`["aud1"]`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`""`))

	// Edge cases
	f.Add([]byte(`null`))
	f.Add([]byte(`42`))
	f.Add([]byte(`[42]`))
	f.Add([]byte(`[null]`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[["nested"]]`))
	f.Add([]byte(`true`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var a Audience
		err := a.UnmarshalJSON(data)
		if err != nil {
			return
		}

		// Round-trip: marshal and re-unmarshal
		marshaled, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal failed after successful unmarshal: %v", err)
		}

		var reparsed Audience
		if err := reparsed.UnmarshalJSON(marshaled); err != nil {
			t.Fatalf("round-trip unmarshal failed: %q -> %v", string(marshaled), err)
		}

		if len(a) != len(reparsed) {
			t.Errorf("round-trip length mismatch: %d vs %d", len(a), len(reparsed))
		}
	})
}

func FuzzTimeUnmarshalJSON(f *testing.F) {
	// Float (unix timestamp)
	f.Add([]byte(`1609459200`))
	f.Add([]byte(`1609459200.5`))
	f.Add([]byte(`0`))
	f.Add([]byte(`-1`))

	// String (ISO8601 / RFC3339)
	f.Add([]byte(`"2021-01-01T00:00:00Z"`))
	f.Add([]byte(`"2021-01-01T00:00:00.000000000Z"`))

	// Edge cases
	f.Add([]byte(`null`))
	f.Add([]byte(`""`))
	f.Add([]byte(`"not-a-date"`))
	f.Add([]byte(`true`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`99999999999999`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var ts Time
		// Must not panic
		_ = ts.UnmarshalJSON(data)
	})
}

func FuzzSpaceDelimitedArrayUnmarshalJSON(f *testing.F) {
	f.Add([]byte(`"openid profile email"`))
	f.Add([]byte(`"openid"`))
	f.Add([]byte(`""`))
	f.Add([]byte(`"a b c d e"`))

	// Edge cases
	f.Add([]byte(`null`))
	f.Add([]byte(`42`))
	f.Add([]byte(`"  "`))
	f.Add([]byte(`" leading"`))
	f.Add([]byte(`"trailing "`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var s SpaceDelimitedArray
		err := s.UnmarshalJSON(data)
		if err != nil {
			return
		}

		// Round-trip
		marshaled, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var reparsed SpaceDelimitedArray
		if err := reparsed.UnmarshalJSON(marshaled); err != nil {
			t.Fatalf("round-trip failed: %q -> %v", string(marshaled), err)
		}

		if len(s) != len(reparsed) {
			t.Errorf("round-trip length mismatch: %d vs %d", len(s), len(reparsed))
		}
	})
}

func FuzzLocalesUnmarshalJSON(f *testing.F) {
	f.Add([]byte(`["en", "fr", "de"]`))
	f.Add([]byte(`"en fr de"`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`""`))
	f.Add([]byte(`null`))

	// Edge cases
	f.Add([]byte(`"invalid-locale-tag-xxxxx"`))
	f.Add([]byte(`[42]`))
	f.Add([]byte(`"en"`))
	f.Add([]byte(`"und"`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var l Locales
		// Must not panic
		_ = l.UnmarshalJSON(data)
	})
}
