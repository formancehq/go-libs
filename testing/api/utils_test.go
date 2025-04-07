package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/formancehq/go-libs/v2/api"
	"github.com/formancehq/go-libs/v2/bun/bunpaginate"
	"github.com/stretchr/testify/require"
)

func TestReadErrorResponse(t *testing.T) {
	errorResp := api.ErrorResponse{
		ErrorCode:    "test_error",
		ErrorMessage: "Test error message",
	}
	
	data, err := json.Marshal(errorResp)
	require.NoError(t, err, "Error marshaling test data")
	
	reader := bytes.NewReader(data)
	
	result := ReadErrorResponse(t, reader)
	
	require.Equal(t, "test_error", result.ErrorCode, "Error code should match")
	require.Equal(t, "Test error message", result.ErrorMessage, "Error message should match")
}

type TestData struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestReadResponse(t *testing.T) {
	testData := &TestData{
		Name: "Test User",
		Age:  30,
	}
	
	resp := api.BaseResponse[*TestData]{
		Data: testData,
	}
	
	data, err := json.Marshal(resp)
	require.NoError(t, err, "Error marshaling test data")
	
	rec := httptest.NewRecorder()
	rec.Body.Write(data)
	
	var result TestData
	
	ReadResponse(t, rec, &result)
	
	require.Equal(t, "Test User", result.Name, "Name should match")
	require.Equal(t, 30, result.Age, "Age should match")
}

func TestReadCursor(t *testing.T) {
	testData := []TestData{
		{Name: "User 1", Age: 25},
		{Name: "User 2", Age: 30},
	}
	
	cursor := bunpaginate.NewCursor(testData, bunpaginate.WithHasMore(true))
	
	resp := api.BaseResponse[TestData]{
		Cursor: cursor,
	}
	
	data, err := json.Marshal(resp)
	require.NoError(t, err, "Error marshaling test data")
	
	rec := httptest.NewRecorder()
	rec.Body.Write(data)
	
	var resultCursor bunpaginate.Cursor[TestData]
	
	ReadCursor(t, rec, &resultCursor)
	
	require.True(t, resultCursor.HasMore, "HasMore should be true")
	require.Len(t, resultCursor.Data, 2, "Should have 2 items")
	require.Equal(t, "User 1", resultCursor.Data[0].Name, "First user name should match")
	require.Equal(t, "User 2", resultCursor.Data[1].Name, "Second user name should match")
}
