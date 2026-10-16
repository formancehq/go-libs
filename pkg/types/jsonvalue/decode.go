// Package jsonvalue decodes single JSON documents without losing number text
// and reads values from dynamically typed JSON trees. It does not define schemas.
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Decode reads exactly one JSON value from data. Numbers in interface values
// are json.Number, including their original exponent and decimal spelling.
// Unknown struct fields are allowed. Callers must bound data before decoding.
// Like encoding/json, it can partially populate out before returning an error,
// accepts null for nullable destinations, and does not reject duplicate keys.
func Decode(data []byte, out any) error {
	return decode(data, out, false)
}

// DecodeStrict also rejects unknown fields in structs. It does not require
// fields, reject null, or validate application-specific values.
func DecodeStrict(data []byte, out any) error {
	return decode(data, out, true)
}

func decode(data []byte, out any, strict bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var extra any
	err := decoder.Decode(&extra)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err == nil:
		return errors.New("decode JSON: multiple JSON values")
	default:
		return fmt.Errorf("decode JSON trailing data: %w", err)
	}
}

// DecodeObject requires an object, including {}. JSON null is not an object.
func DecodeObject(data []byte) (map[string]any, error) {
	var value any
	if err := Decode(data, &value); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("decode JSON: expected object")
	}
	return object, nil
}
