package api

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/formancehq/go-libs/v3/bun/bunpaginate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Encode(t require.TestingT, v interface{}) []byte {
	data, err := json.Marshal(v)
	assert.NoError(t, err)
	return data
}

func Buffer(t require.TestingT, v interface{}) *bytes.Buffer {
	return bytes.NewBuffer(Encode(t, v))
}

func Decode(t require.TestingT, reader io.Reader, v interface{}) {
	err := json.NewDecoder(reader).Decode(v)
	require.NoError(t, err)
}

func DecodeSingleResponse[T any](t require.TestingT, reader io.Reader) (T, bool) {
	res := BaseResponse[T]{}
	Decode(t, reader, &res)
	return *res.Data, true
}

func DecodeCursorResponse[T any](t require.TestingT, reader io.Reader) *bunpaginate.Cursor[T] {
	res := BaseResponse[T]{}
	Decode(t, reader, &res)
	return res.Cursor
}
