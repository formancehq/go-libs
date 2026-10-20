package redact

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestJSONRefusesRewrittenObjectKeys(t *testing.T) {
	ConfigureValues(nil)
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, input := range []string{
		`{ "Bearer alpha":1, "Bearer beta":2, "safe":"\\u0061", "n":1e+09 }`,
		`{"outer":[{"Bearer alpha":1,"Bearer beta":2}],"safe":true}`,
		`{"Bearer alpha":1}`,
	} {
		original := []byte(input)
		got, ok := JSON(original)
		if ok || got != nil {
			t.Errorf("capture accepted rewritten keys: %q", got)
		}
		if string(original) != input {
			t.Fatal("capture mutated caller bytes")
		}
	}
}

func TestJSONPreservesOriginalKeyTokens(t *testing.T) {
	ConfigureValues(nil)
	t.Cleanup(func() { ConfigureValues(nil) })
	input := []byte(`{ "a":1,"a":2,"escaped\u0061":[{"password":"hidden"}],"safe":"\\u0061", "n":1e+09 }`)
	original := bytes.Clone(input)
	want := bytes.Replace(input, []byte(`"hidden"`), []byte(`"[REDACTED]"`), 1)
	got, ok := JSON(input)
	if !ok || !json.Valid(got) || !bytes.Equal(got, want) || !bytes.Equal(input, original) {
		t.Fatalf("safe capture changed original structure: %q, %v", got, ok)
	}
}
