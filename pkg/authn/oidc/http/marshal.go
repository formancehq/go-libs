package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
)

func MarshalJSON(w http.ResponseWriter, i any) {
	MarshalJSONWithStatus(w, i, http.StatusOK)
}

func MarshalJSONWithStatus(w http.ResponseWriter, i any, status int) {
	w.Header().Set("content-type", "application/json")
	if isNilJSONValue(i) {
		w.WriteHeader(status)
		return
	}
	data, err := json.Marshal(i)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func isNilJSONValue(i any) bool {
	if i == nil {
		return true
	}
	value := reflect.ValueOf(i)
	switch value.Kind() {
	case reflect.Pointer:
		return value.IsNil()
	default:
		return false
	}
}

func ConcatenateJSON(first, second []byte) ([]byte, error) {
	if !bytes.HasSuffix(first, []byte{'}'}) {
		return nil, fmt.Errorf("jws: invalid JSON %s", first)
	}
	if !bytes.HasPrefix(second, []byte{'{'}) {
		return nil, fmt.Errorf("jws: invalid JSON %s", second)
	}
	// check empty
	if len(first) == 2 {
		return second, nil
	}
	if len(second) == 2 {
		return first, nil
	}

	first[len(first)-1] = ','
	first = append(first, second[1:]...)
	return first, nil
}
