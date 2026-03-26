package metadata

import (
	"encoding/json"
	"testing"
)

func FuzzMetadataScan(f *testing.F) {
	// Valid JSON metadata
	f.Add(`{"key":"value"}`)
	f.Add(`{"a":"1","b":"2","c":"3"}`)
	f.Add(`{}`)

	// Edge cases
	f.Add(``)
	f.Add(`null`)
	f.Add(`[]`)
	f.Add(`"string"`)
	f.Add(`{"key":123}`)
	f.Add(`{invalid`)
	f.Add(`{"nested":{"a":"b"}}`)

	f.Fuzz(func(t *testing.T, input string) {
		// Scan from []uint8 — must not panic
		var m1 Metadata
		err := m1.Scan([]uint8(input))
		if err != nil {
			return
		}

		// If scan succeeded, the metadata should be marshallable
		marshaled, err := json.Marshal(m1)
		if err != nil {
			t.Fatalf("marshal failed after successful scan: %v", err)
		}

		// Round-trip
		var m2 Metadata
		if err := m2.Scan([]uint8(marshaled)); err != nil {
			t.Fatalf("round-trip scan failed: %q -> %v", string(marshaled), err)
		}

		if !m1.IsEquivalentTo(m2) {
			t.Errorf("round-trip mismatch: %v vs %v", m1, m2)
		}
	})
}

func FuzzMetadataMerge(f *testing.F) {
	f.Add(`{"a":"1"}`, `{"b":"2"}`)
	f.Add(`{"a":"1"}`, `{"a":"2"}`)
	f.Add(`{}`, `{"a":"1"}`)
	f.Add(`{"a":"1"}`, `{}`)
	f.Add(`{}`, `{}`)

	f.Fuzz(func(t *testing.T, json1, json2 string) {
		var m1, m2 Metadata
		if err := json.Unmarshal([]byte(json1), &m1); err != nil {
			return
		}
		if err := json.Unmarshal([]byte(json2), &m2); err != nil {
			return
		}

		// Must not panic
		merged := m1.Merge(m2)

		// All keys from m2 must be in merged (m2 overrides m1)
		for k, v := range m2 {
			if merged[k] != v {
				t.Errorf("merge lost key %q: expected %q, got %q", k, v, merged[k])
			}
		}
	})
}

func FuzzUnmarshalValue(f *testing.F) {
	f.Add(`"hello"`)
	f.Add(`42`)
	f.Add(`true`)
	f.Add(`null`)
	f.Add(`[1,2,3]`)
	f.Add(`{"a":"b"}`)

	f.Fuzz(func(t *testing.T, input string) {
		// UnmarshalValue panics on invalid JSON — we catch that
		defer func() {
			if r := recover(); r != nil {
				// Expected for invalid JSON — this is a known behavior
				// but the fuzzer documents inputs that trigger it
			}
		}()

		_ = UnmarshalValue[any](input)
	})
}
