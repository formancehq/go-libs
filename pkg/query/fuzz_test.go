package query

import (
	"encoding/json"
	"testing"
)

func FuzzParseJSON(f *testing.F) {
	// Valid expressions
	f.Add(`{"$match": {"account": "users:001"}}`)
	f.Add(`{"$and": [{"$match": {"account": "pending"}}, {"$gte": {"balance": 100}}]}`)
	f.Add(`{"$or": [{"$match": {"a": "b"}}, {"$match": {"c": "d"}}]}`)
	f.Add(`{"$not": {"$match": {"account": "blocked"}}}`)
	f.Add(`{"$like": {"name": "john%"}}`)
	f.Add(`{"$in": {"account": ["A", "B"]}}`)
	f.Add(`{"$lt": {"balance": 0}}`)
	f.Add(`{"$lte": {"balance": 50}}`)
	f.Add(`{"$gt": {"balance": 100}}`)
	f.Add(`{"$gte": {"balance": 288230376151711747}}`)
	f.Add(`{"$exists": {"metadata": true}}`)

	// Nested
	f.Add(`{"$and": [{"$or": [{"$match": {"a": "1"}}, {"$match": {"b": "2"}}]}, {"$not": {"$match": {"c": "3"}}}]}`)

	// Edge cases
	f.Add(`{}`)
	f.Add(``)
	f.Add(`null`)
	f.Add(`[]`)
	f.Add(`"string"`)
	f.Add(`42`)
	f.Add(`{"$unknown": {"key": "val"}}`)
	f.Add(`{"$match": "not_a_map"}`)
	f.Add(`{"$and": "not_an_array"}`)
	f.Add(`{"$and": []}`)
	f.Add(`{`)
	f.Add(`{"a": 1, "b": 2}`)

	f.Fuzz(func(t *testing.T, data string) {
		// Must not panic
		builder, err := ParseJSON(data)
		if err != nil {
			return
		}

		if builder == nil {
			return
		}

		// If parsing succeeded, Build must not panic
		_, _, _ = builder.Build(ContextFn(func(key, operator string, value any) (string, []any, error) {
			return "1 = 1", nil, nil
		}))

		// If parsing succeeded, MarshalJSON must not panic
		marshaled, err := json.Marshal(builder)
		if err != nil {
			return
		}

		// Round-trip: re-parse the marshaled output
		reparsed, err := ParseJSON(string(marshaled))
		if err != nil {
			t.Fatalf("round-trip parse failed: marshal produced %q, reparse error: %v", string(marshaled), err)
		}

		if reparsed == nil {
			t.Fatal("round-trip produced nil builder from non-nil marshal output")
		}
	})
}
