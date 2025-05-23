package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/formancehq/go-libs/v3/bun/bunpaginate"

	"github.com/formancehq/go-libs/v3/api"
	"github.com/stretchr/testify/require"
)

func ReadErrorResponse(t *testing.T, r io.Reader) *api.ErrorResponse {
	t.Helper()
	ret := &api.ErrorResponse{}
	require.NoError(t, json.NewDecoder(r).Decode(ret))
	return ret
}

func ReadResponse[T any](t *testing.T, rec *httptest.ResponseRecorder, to T) {
	t.Helper()
	ret := &api.BaseResponse[T]{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(ret))
	reflect.ValueOf(to).Elem().Set(reflect.ValueOf(*ret.Data).Elem())
}

func ReadCursor[T any](t *testing.T, rec *httptest.ResponseRecorder, to *bunpaginate.Cursor[T]) {
	t.Helper()
	ret := &api.BaseResponse[T]{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(ret))
	reflect.ValueOf(to).Elem().Set(reflect.ValueOf(ret.Cursor).Elem())
}
