package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalJSONWithStatusWritesStatusForNilBody(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()

	MarshalJSONWithStatus(recorder, nil, stdhttp.StatusNoContent)

	require.Equal(t, stdhttp.StatusNoContent, recorder.Code)
	require.Empty(t, recorder.Body.String())
	require.Equal(t, "application/json", recorder.Header().Get("content-type"))
}

func TestMarshalJSONWithStatusWritesStatusForTypedNilBody(t *testing.T) {
	t.Parallel()

	var body *struct {
		ID string `json:"id"`
	}
	recorder := httptest.NewRecorder()

	MarshalJSONWithStatus(recorder, body, stdhttp.StatusAccepted)

	require.Equal(t, stdhttp.StatusAccepted, recorder.Code)
	require.Empty(t, recorder.Body.String())
}
