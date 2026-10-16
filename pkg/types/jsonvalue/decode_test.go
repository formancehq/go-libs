package jsonvalue_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/formancehq/go-libs/v5/pkg/types/jsonvalue"
)

func TestSingleDocumentAndExactNumbers(t *testing.T) {
	t.Parallel()
	for _, decode := range []func([]byte, any) error{jsonvalue.Decode, jsonvalue.DecodeStrict} {
		var value map[string]any
		if err := decode([]byte(" \n {\"integer\":9007199254740997,\"decimal\":1.2300,\"exponent\":1e+40,\"nested\":[-0]} \t"), &value); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"integer": json.Number("9007199254740997"), "decimal": json.Number("1.2300"), "exponent": json.Number("1e+40"), "nested": []any{json.Number("-0")}}
		if !reflect.DeepEqual(value, want) {
			t.Fatalf("decoded %#v; want %#v", value, want)
		}
		for _, input := range []string{"", " \n ", "{", "[1,]", "true false", "{} null", "{} garbage", "{} /* comment */", "{}\x00"} {
			var out any
			if err := decode([]byte(input), &out); err == nil {
				t.Errorf("accepted malformed or multiple document %q", input)
			}
		}
	}
}

func TestStructDecodePolicyAndPartialOutput(t *testing.T) {
	t.Parallel()
	type record struct {
		ID string `json:"id"`
	}
	var additive record
	if err := jsonvalue.Decode([]byte(`{"id":"r1","future":true}`), &additive); err != nil || additive.ID != "r1" {
		t.Fatalf("additive=%#v, error=%v", additive, err)
	}
	var strict record
	if err := jsonvalue.DecodeStrict([]byte(`{"id":"r1","future":true}`), &strict); err == nil || strict.ID != "r1" {
		t.Fatalf("strict partial output=%#v, error=%v", strict, err)
	}
	for _, decode := range []func([]byte, any) error{jsonvalue.Decode, jsonvalue.DecodeStrict} {
		out := record{ID: "default"}
		if err := decode([]byte(`null`), &out); err != nil || out.ID != "default" {
			t.Fatalf("null output=%#v, error=%v", out, err)
		}
		if err := decode([]byte(`{"id":"first","id":"last"}`), &out); err != nil || out.ID != "last" {
			t.Fatalf("duplicate keys no longer follow encoding/json: %#v %v", out, err)
		}
		if err := decode([]byte(`{"id":"changed"} []`), &out); err == nil || out.ID != "changed" {
			t.Fatalf("trailing value output=%#v, error=%v", out, err)
		}
		if err := decode([]byte(`{"id":true}`), &out); err == nil {
			t.Fatal("accepted wrong field type")
		} else if _, ok := errors.AsType[*json.UnmarshalTypeError](err); !ok {
			t.Fatalf("lost wrapped type error: %v", err)
		}
		if err := decode([]byte(`{}`), nil); err == nil {
			t.Fatal("accepted invalid destination")
		}
	}
}

func TestDecodeShapes(t *testing.T) {
	t.Parallel()
	object, err := jsonvalue.DecodeObject([]byte(`{}`))
	if err != nil || object == nil || len(object) != 0 {
		t.Fatalf("empty object=%#v error=%v", object, err)
	}
	for _, input := range []string{`null`, `[]`, `true`, `"text"`, `12`, `{} {}`, `{} false`} {
		if _, err := jsonvalue.DecodeObject([]byte(input)); err == nil {
			t.Errorf("object accepted %s", input)
		}
	}

}

func TestDecodeErrorTextAndChain(t *testing.T) {
	t.Parallel()
	var out any
	if err := jsonvalue.Decode([]byte(`true false`), &out); err == nil || err.Error() != "decode JSON: multiple JSON values" {
		t.Fatalf("multiple values: %v", err)
	}
	for _, input := range []string{"{", "{} !"} {
		err := jsonvalue.Decode([]byte(input), &out)
		if err == nil || !strings.HasPrefix(err.Error(), "decode JSON") {
			t.Fatalf("missing decode context: %v", err)
		}
	}
	err := jsonvalue.Decode([]byte(`{} !`), &out)
	if _, ok := errors.AsType[*json.SyntaxError](err); !ok || !strings.HasPrefix(err.Error(), "decode JSON trailing data:") {
		t.Fatalf("missing trailing syntax context/cause: %v", err)
	}
}
