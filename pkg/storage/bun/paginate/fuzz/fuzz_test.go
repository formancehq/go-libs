package fuzz

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/formancehq/go-libs/v5/pkg/storage/bun/paginate"
)

func FuzzUnmarshalCursor(f *testing.F) {
	// Valid cursors (base64url encoded JSON)
	validJSON := []string{
		`{"pageSize":15,"column":"id","order":0}`,
		`{"offset":0,"pageSize":100}`,
		`{"pageSize":15,"bottom":null,"paginationID":null}`,
		`{}`,
		`{"pageSize":0}`,
		`null`,
	}
	for _, j := range validJSON {
		f.Add(base64.RawURLEncoding.EncodeToString([]byte(j)))
	}

	// Edge cases
	f.Add("")
	f.Add("not-base64!")
	f.Add("====")
	f.Add(base64.RawURLEncoding.EncodeToString([]byte(`{`)))
	f.Add(base64.RawURLEncoding.EncodeToString([]byte(`[]`)))

	f.Fuzz(func(t *testing.T, cursor string) {
		var target map[string]any

		// Must not panic
		err := paginate.UnmarshalCursor(cursor, &target)
		if err != nil {
			return
		}

		// Round-trip: encode back and re-decode
		encoded := paginate.EncodeCursor(target)
		var reparsed map[string]any
		if err := paginate.UnmarshalCursor(encoded, &reparsed); err != nil {
			t.Fatalf("round-trip failed: encoded=%q err=%v", encoded, err)
		}
	})
}

func FuzzBigIntFromString(f *testing.F) {
	f.Add("0")
	f.Add("1")
	f.Add("-1")
	f.Add("999999999999999999999999999999")
	f.Add("-999999999999999999999999999999")
	f.Add("")
	f.Add("abc")
	f.Add("12.34")
	f.Add("0x1F")
	f.Add(" 123")
	f.Add("123 ")

	f.Fuzz(func(t *testing.T, input string) {
		bi := paginate.NewInt()

		// Must not panic
		result, err := bi.FromString(input)
		if err != nil {
			return
		}

		if result == nil {
			t.Fatal("nil result without error")
		}

		// Round-trip via JSON
		marshaled, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var reparsed paginate.BigInt
		if err := json.Unmarshal(marshaled, &reparsed); err != nil {
			t.Fatalf("round-trip unmarshal failed: %q -> %v", string(marshaled), err)
		}

		if result.Cmp(&reparsed) != 0 {
			t.Errorf("round-trip mismatch: %v vs %v", result.ToMathBig(), reparsed.ToMathBig())
		}
	})
}
